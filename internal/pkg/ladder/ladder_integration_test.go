//go:build integration

package ladder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// runEcho compiles the two .erl files and boots echoserver:start + echoclient:main.
func runEcho(t *testing.T, serverErl, clientErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	for _, src := range []string{serverErl, clientErl} {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}
	// boot: start server, run client, halt.
	eval := "echoserver:start(), echoclient:main(), init:stop()."
	cmd := exec.Command(l.Erl(), "-noshell", "-pa", work, "-eval", eval)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("erl: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRung1_ErlangToErlang(t *testing.T) {
	got := runEcho(t,
		filepath.FromSlash("../../../testdata/echo/erlang/echoserver.erl"),
		filepath.FromSlash("../../../testdata/echo/erlang/echoclient.erl"))
	if got != "hello" {
		t.Fatalf("echo = %q, want %q", got, "hello")
	}
}

// transpileToErl transpiles the Go fixture at goPath and writes the result
// as <dir>/<mod>.erl, returning the written path.
func transpileToErl(t *testing.T, goPath, dir string) string {
	t.Helper()
	src, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	erl, mod, err := transpile.File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, mod+".erl")
	if err := os.WriteFile(out, []byte(erl), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRung2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/echo/go/echoclient/main.go", dir)
	got := runEcho(t, "../../../testdata/echo/erlang/echoserver.erl", client)
	if got != "hello" {
		t.Fatalf("rung2 echo = %q", got)
	}
}

func TestRung3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo/go/echoserver/main.go", dir)
	got := runEcho(t, server, "../../../testdata/echo/erlang/echoclient.erl")
	if got != "hello" {
		t.Fatalf("rung3 echo = %q", got)
	}
}

func TestRung4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo/go/echoserver/main.go", dir)
	client := transpileToErl(t, "../../../testdata/echo/go/echoclient/main.go", dir)
	got := runEcho(t, server, client)
	if got != "hello" {
		t.Fatalf("rung4 echo = %q", got)
	}
}

// TestRung_ValueModel transpiles the 0.3.1 value-model fixture (parameters,
// a local binding, a call with arguments) and proves the emitted Erlang
// actually compiles with erlc — green unit tests are not enough.
func TestRung_ValueModel(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	dir := t.TempDir()
	erl := transpileToErl(t, filepath.FromSlash("../../../testdata/valuemodel/math.go"), dir)
	if out, err := exec.Command(l.Erlc(), "-o", dir, erl).CombinedOutput(); err != nil {
		t.Fatalf("erlc %s: %v\n%s", erl, err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "math.beam")); err != nil {
		t.Fatalf("no math.beam produced: %v", err)
	}
}
