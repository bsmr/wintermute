//go:build integration

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
)

// TestReleaseWithNativeErlModule drives the real `wm release --self-contained`
// with a MIXED input set: the app and supervisor are transpiled Go, the echo
// server is a hand-written .erl module (record + guard). It boots the target
// system under a fully scrubbed environment (no system Erlang) and drives a
// scrubbed control node to resolve {global, echo} and call it — proving a native
// .erl input survives buildApp routing, erlc, release + target-system assembly,
// and boots interoperably with transpiled Go.
func TestReleaseWithNativeErlModule(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skipf("local Erlang %s not installed", erlang.DefaultVersion)
	}
	out := t.TempDir()
	vsn := "0.2.7"
	srcs := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
		"../../../testdata/native/echoserver.erl", // native, swapped in for the Go server
	}
	ctx := context.Background()

	args := append([]string{"release", "--out", out, "--vsn", vsn, "--self-contained"}, srcs...)
	if err := Run(ctx, args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("wm release --self-contained: %v", err)
	}

	unpack := t.TempDir()
	f, err := os.Open(filepath.Join(out, "echoapp-"+vsn+".tar.gz"))
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if err := release.Untar(f, unpack); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	target := filepath.Join(unpack, "echoapp-"+vsn)

	// Native module's .beam must be in the bundle.
	if _, err := os.Stat(filepath.Join(target, "lib", "echoapp-"+vsn, "ebin", "echoserver.beam")); err != nil {
		t.Fatalf("native echoserver.beam not packaged: %v", err)
	}

	node := fmt.Sprintf("wmnat_%d@127.0.0.1", os.Getpid())
	if err := os.WriteFile(filepath.Join(target, "releases", vsn, "vm.args"), []byte("-name "+node+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scrub := []string{"PATH=/usr/bin:/bin", "HOME=" + target}
	erlBin := filepath.Join(target, "erts-"+mustErts(t, target), "bin", "erl")

	start := exec.CommandContext(ctx, filepath.Join(target, "bin", "start"))
	start.Env = scrub
	if o, err := start.CombinedOutput(); err != nil {
		t.Fatalf("bin/start: %v\n%s", err, o)
	}
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		_ = stop.Run()
	})

	callEval := fmt.Sprintf(
		`Wait = fun Loop(0) -> erlang:error(timeout); Loop(N) -> net_adm:ping('%s'), global:sync(), `+
			`case global:whereis_name(echo) of undefined -> timer:sleep(100), Loop(N-1); _ -> ok end end, `+
			`Wait(30), R = gen_server:call({global,echo}, <<"hello">>), io:format("~s", [R]), init:stop().`, node)
	ctrl := exec.CommandContext(ctx, erlBin,
		"-boot", filepath.Join(target, "releases", vsn, "start_clean"),
		"-name", fmt.Sprintf("wmnatctrl_%d@127.0.0.1", os.Getpid()), "-noshell", "-eval", callEval)
	ctrl.Env = scrub
	got, err := ctrl.CombinedOutput()
	if err != nil {
		t.Fatalf("scrubbed control node: %v\n%s", err, got)
	}
	if strings.TrimSpace(string(got)) != "hello" {
		t.Fatalf("native-module reply = %q, want hello", got)
	}
}
