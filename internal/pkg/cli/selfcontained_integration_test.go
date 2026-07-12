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

// TestSelfContainedTargetSystemEndToEnd is rung VII: it drives the real
// `wm release --self-contained`, unpacks the artifact into a fresh dir, boots
// bin/start under a FULLY SCRUBBED environment (env with only PATH=/usr/bin:/bin
// and HOME set — no system Erlang, no ERL_*/ROOTDIR, no -setcookie so erl uses the
// bundle's own ~/.erlang.cookie), and drives a scrubbed control node (booted with
// the bundled start_clean) to resolve {global, echo} and call it. Proves the
// target system is self-contained without needing an Erlang-free machine.
func TestSelfContainedTargetSystemEndToEnd(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skipf("local Erlang %s not installed", erlang.DefaultVersion)
	}
	out := t.TempDir()
	vsn := "0.2.6"
	srcs := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
		"../../../testdata/persistent/go/echoserver/main.go",
	}
	ctx := context.Background()

	// 1. Build the self-contained target system via the real CLI.
	args := append([]string{"release", "--out", out, "--vsn", vsn, "--self-contained"}, srcs...)
	if err := Run(ctx, args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("wm release --self-contained: %v", err)
	}

	// 2. Unpack the artifact into a fresh HOME (isolated, auto-created ~/.erlang.cookie).
	// The tarball unpacks into a self-named <app>-<vsn>/ directory.
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

	// Pin a unique node name (rewrite the bundled vm.args) to avoid epmd clashes.
	node := fmt.Sprintf("wmsc_%d@127.0.0.1", os.Getpid())
	if err := os.WriteFile(filepath.Join(target, "releases", vsn, "vm.args"), []byte("-name "+node+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scrub := []string{"PATH=/usr/bin:/bin", "HOME=" + target}
	erlBin := filepath.Join(target, "erts-"+mustErts(t, target), "bin", "erl")

	// 3. Boot bin/start under a SCRUBBED environment (no system Erlang).
	start := exec.CommandContext(ctx, filepath.Join(target, "bin", "start"))
	start.Env = scrub
	if o, err := start.CombinedOutput(); err != nil {
		t.Fatalf("bin/start: %v\n%s", err, o)
	}
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		_ = stop.Run()
		// Fallback: SIGKILL any beam still rooted at this test's unique dir. bin/stop
		// exits 0 even when its rpc:call never reaches the node (init:stop/0 is async,
		// and this test overwrites vm.args with a fresh node name after the release was
		// built, so bin/stop's baked-in rpc target is stale) — a clean exit status does
		// not guarantee the node actually died, so always sweep instead of gating on err.
		_ = exec.Command("pkill", "-9", "-f", unpack).Run()
	})

	// 4. Scrubbed control node resolves {global, echo} and calls it.
	callEval := fmt.Sprintf(
		`Wait = fun Loop(0) -> erlang:error(timeout); Loop(N) -> net_adm:ping('%s'), global:sync(), `+
			`case global:whereis_name(echo) of undefined -> timer:sleep(100), Loop(N-1); _ -> ok end end, `+
			`Wait(30), R = gen_server:call({global,echo}, <<"hello">>), io:format("~s", [R]), init:stop().`, node)
	ctrl := exec.CommandContext(ctx, erlBin,
		"-boot", filepath.Join(target, "releases", vsn, "start_clean"),
		"-name", fmt.Sprintf("wmscctrl_%d@127.0.0.1", os.Getpid()), "-noshell", "-eval", callEval)
	ctrl.Env = scrub
	got, err := ctrl.CombinedOutput()
	if err != nil {
		t.Fatalf("scrubbed control node: %v\n%s", err, got)
	}
	if strings.TrimSpace(string(got)) != "hello" {
		t.Fatalf("self-contained reply = %q, want hello", got)
	}
}

func mustErts(t *testing.T, root string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(root, "erts-*"))
	if len(m) != 1 {
		t.Fatalf("expected one erts-* in bundle, got %v", m)
	}
	return strings.TrimPrefix(filepath.Base(m[0]), "erts-")
}
