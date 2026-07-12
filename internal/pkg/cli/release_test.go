package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubErlc records erlc calls and creates the .beam so downstream steps proceed,
// without a real toolchain.
func stubErlc(t *testing.T) {
	t.Helper()
	orig := runErl
	runErl = func(_ context.Context, _, name string, a ...string) error {
		if strings.HasSuffix(name, "erlc") {
			// -o <dir> <src.erl>: fabricate <dir>/<mod>.beam
			dir, src := a[1], a[2]
			mod := strings.TrimSuffix(filepath.Base(src), ".erl")
			return os.WriteFile(filepath.Join(dir, mod+".beam"), []byte("BEAM"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { runErl = orig })
}

func TestReleaseBuildsTree(t *testing.T) {
	stubErlc(t)
	// Mock captureErl to avoid running the real erl binary.
	orig := captureErl
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = orig })

	// A fake OTP install so ErtsVersion/AppVersion resolve.
	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	src := writeEchoAppSources(t) // helper below returns []string of .go paths
	out := filepath.Join(t.TempDir(), "rel")
	var buf bytes.Buffer
	args := append([]string{"--out", out, "--vsn", "0.2.5", "--name", "echo@127.0.0.1"}, src...)
	if err := releaseCmd(context.Background(), args, &buf); err != nil {
		t.Fatalf("releaseCmd: %v", err)
	}

	must := []string{
		"wm.json",
		"lib/echo-0.2.5/ebin/echo.app",
		"releases/0.2.5/echo.rel",
		"releases/0.2.5/sys.config",
		"releases/0.2.5/vm.args",
	}
	for _, p := range must {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	rel, _ := os.ReadFile(filepath.Join(out, "releases/0.2.5/echo.rel"))
	if !strings.Contains(string(rel), `{erts, "17.0.3"}`) ||
		!strings.Contains(string(rel), `{kernel, "11.0.3"}`) ||
		!strings.Contains(string(rel), `{stdlib, "8.0.2"}`) ||
		!strings.Contains(string(rel), `{echo, "0.2.5"}`) {
		t.Errorf("echo.rel missing discovered versions:\n%s", rel)
	}
	vm, _ := os.ReadFile(filepath.Join(out, "releases/0.2.5/vm.args"))
	if strings.Contains(string(vm), "setcookie") {
		t.Errorf("vm.args must not carry a cookie:\n%s", vm)
	}
}

// TestReleaseRejectsTraversalVsn guards the write-side counterpart to startCmd's
// validAppName(m.Vsn): a crafted --vsn (or a poisoned VERSION file) must not
// escape the release dir. The vsn guard fires after transpile but before any
// path is built or erlc runs, so no OTP install is needed here.
func TestReleaseRejectsTraversalVsn(t *testing.T) {
	src := writeEchoAppSources(t)
	out := filepath.Join(t.TempDir(), "rel")
	var buf bytes.Buffer
	args := append([]string{"--out", out, "--vsn", "../../pwn", "--name", "echo@127.0.0.1"}, src...)
	err := releaseCmd(context.Background(), args, &buf)
	if err == nil || !strings.Contains(err.Error(), "invalid vsn") {
		t.Fatalf("releaseCmd with traversal --vsn: err = %v, want 'invalid vsn'", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "releases")); statErr == nil {
		t.Fatalf("traversal --vsn built a releases dir; guard fired too late")
	}
}

func TestReleaseCleansStageOnError(t *testing.T) {
	// Stub erlc to return an error, simulating a compilation failure.
	orig := runErl
	runErl = func(_ context.Context, _, name string, a ...string) error {
		if strings.HasSuffix(name, "erlc") {
			return fmt.Errorf("erlc failed")
		}
		return nil
	}
	t.Cleanup(func() { runErl = orig })

	// Mock captureErl to avoid running the real erl binary.
	origErl := captureErl
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = origErl })

	// A fake OTP install so ErtsVersion/AppVersion resolve.
	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	src := writeEchoAppSources(t)
	out := t.TempDir()
	var buf bytes.Buffer
	args := append([]string{"--out", out, "--vsn", "0.2.5"}, src...)
	err := releaseCmd(context.Background(), args, &buf)
	if err == nil {
		t.Fatal("releaseCmd: expected error, got nil")
	}

	// Verify .stage does not exist after the error.
	stage := filepath.Join(out, ".stage")
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Errorf(".stage was not cleaned up: os.Stat returned %v", err)
	}
}

func TestReleaseInvokesMakeScript(t *testing.T) {
	stubErlc(t)
	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		os.MkdirAll(filepath.Join(lib, d), 0o755)
	}
	t.Setenv("HOME", home)

	var gotDir, gotEval string
	orig := captureErl
	captureErl = func(_ context.Context, dir, _ string, a ...string) ([]byte, error) {
		gotDir = dir
		for i, x := range a {
			if x == "-eval" {
				gotEval = a[i+1]
			}
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = orig })

	out := filepath.Join(t.TempDir(), "rel")
	src := writeEchoAppSources(t)
	args := append([]string{"--out", out, "--vsn", "0.2.5"}, src...)
	if err := releaseCmd(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if gotDir != filepath.Join(out, "releases", "0.2.5") {
		t.Errorf("make_script cwd = %q", gotDir)
	}
	if !strings.Contains(gotEval, `systools:make_script("echo"`) {
		t.Errorf("eval missing make_script:\n%s", gotEval)
	}
}

func TestReleaseTarInvokesMakeTar(t *testing.T) {
	stubErlc(t)
	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		os.MkdirAll(filepath.Join(lib, d), 0o755)
	}
	t.Setenv("HOME", home)

	var evals []string
	orig := captureErl
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		for i, x := range a {
			if x == "-eval" {
				evals = append(evals, a[i+1])
			}
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = orig })

	out := filepath.Join(t.TempDir(), "rel")
	src := writeEchoAppSources(t)
	args := append([]string{"--out", out, "--vsn", "0.2.5", "--tar"}, src...)
	if err := releaseCmd(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var sawTar bool
	for _, e := range evals {
		if strings.Contains(e, `systools:make_tar("echo"`) {
			sawTar = true
		}
	}
	if !sawTar {
		t.Errorf("--tar did not invoke make_tar; evals=%v", evals)
	}
}

// writeEchoAppSources writes a minimal application module (package echo, an
// application behaviour) to a temp .go file and returns its path.
//
// NOTE: this deliberately does NOT reuse testdata/persistent/go/echoapp/main.go
// — that fixture's package is "echoapp", which would make appMod "echoapp" and
// break the "echo"-named assertions above (lib/echo-0.2.5/ebin/echo.app etc.).
// This is the same application shape (Start/Stop over otp.StartSupervisor),
// just under package echo so the app name matches.
func writeEchoAppSources(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "echo.go")
	body := `package echo

import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/persistent/go/echosup"
)

type App struct{}

func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
`
	if err := os.WriteFile(app, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{app}
}
