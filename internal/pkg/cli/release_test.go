package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/release"
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

// TestReleaseRejectsShellMetacharVsn guards against shell injection: vsn is
// spliced raw into the generated /bin/sh launcher scripts, so a shell
// metacharacter must be rejected (validAppName alone allowed it — it only blocks
// path separators).
func TestReleaseRejectsShellMetacharVsn(t *testing.T) {
	for _, bad := range []string{"$(touch pwned)", "1.0`id`", `a"b`, "a b"} {
		src := writeEchoAppSources(t)
		out := filepath.Join(t.TempDir(), "rel")
		var buf bytes.Buffer
		args := append([]string{"--out", out, "--vsn", bad, "--name", "echo@127.0.0.1"}, src...)
		err := releaseCmd(context.Background(), args, &buf)
		if err == nil || !strings.Contains(err.Error(), "invalid vsn") {
			t.Errorf("releaseCmd --vsn %q: err = %v, want 'invalid vsn'", bad, err)
		}
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

func TestReleaseSelfContainedRelAndEvals(t *testing.T) {
	stubErlc(t)
	var evals []string
	orig := captureErl
	captureErl = func(_ context.Context, dir, _ string, a ...string) ([]byte, error) {
		joined := strings.Join(a, " ")
		for i, x := range a {
			if x == "-eval" {
				evals = append(evals, a[i+1])
			}
		}
		// The self-contained path now also runs assembleTargetSystem after
		// make_tar; materialise what it expects to unpack/regenerate so the
		// wiring under test (not just the eval strings) runs to completion.
		switch {
		case strings.Contains(joined, "make_tar"):
			writeFakeBundleTar(t, filepath.Join(dir, "echo.tar.gz"), "0.2.6")
		case strings.Contains(joined, `make_script("start_clean"`):
			os.WriteFile(filepath.Join(dir, "start_clean.boot"), []byte("CLEAN"), 0o644)
		case strings.Contains(joined, "create_RELEASES"):
			os.MkdirAll(filepath.Join(dir, "releases"), 0o755)
			os.WriteFile(filepath.Join(dir, "releases", "RELEASES"), []byte("RELEASES"), 0o644)
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = orig })

	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2", "lib/sasl-4.4"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		if err := os.MkdirAll(filepath.Join(lib, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lib, "bin", f), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	src := writeEchoAppSources(t)
	out := filepath.Join(t.TempDir(), "rel")
	args := append([]string{"--out", out, "--vsn", "0.2.6", "--self-contained"}, src...)
	if err := releaseCmd(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatalf("releaseCmd --self-contained: %v", err)
	}

	// .rel must include sasl.
	rel, _ := os.ReadFile(filepath.Join(out, "releases/0.2.6/echo.rel"))
	if !strings.Contains(string(rel), "{sasl,") {
		t.Errorf("self-contained .rel must include sasl:\n%s", rel)
	}
	// make_script must be NON-local; make_tar must bundle erts.
	var sawScript, sawTar bool
	for _, e := range evals {
		if strings.Contains(e, "make_script") {
			sawScript = true
			if strings.Contains(e, "local") {
				t.Errorf("self-contained make_script must NOT use local:\n%s", e)
			}
		}
		if strings.Contains(e, "make_tar") {
			sawTar = true
			if !strings.Contains(e, "{erts,") {
				t.Errorf("self-contained make_tar must bundle erts:\n%s", e)
			}
		}
	}
	if !sawScript || !sawTar {
		t.Errorf("expected make_script and make_tar evals; got %v", evals)
	}
}

func TestReleaseSelfContainedAssemblesTargetSystem(t *testing.T) {
	stubErlc(t)
	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2", "lib/sasl-4.4"} {
		os.MkdirAll(filepath.Join(lib, d), 0o755)
	}
	// Fake OTP bin/ launchers WriteLauncherLayout will copy.
	for _, f := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		os.WriteFile(filepath.Join(lib, "bin", f), []byte("x"), 0o755)
	}
	os.MkdirAll(filepath.Join(lib, "bin"), 0o755)
	for _, f := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		os.WriteFile(filepath.Join(lib, "bin", f), []byte("x"), 0o755)
	}
	t.Setenv("HOME", home)

	out := filepath.Join(t.TempDir(), "rel")

	// captureErl stub: on make_tar, write a realistic bundle tar.gz to relDir; on
	// make_script for start_clean, write a fake start_clean.boot; on create_RELEASES,
	// write a fake releases/RELEASES so the final repack has one to bundle
	// (the brief's stub note calls this step a "no-op" for the erl side, but the
	// unpacked target system must still end up with the file the assertion below
	// checks for).
	orig := captureErl
	captureErl = func(_ context.Context, dir, _ string, a ...string) ([]byte, error) {
		joined := strings.Join(a, " ")
		switch {
		case strings.Contains(joined, "make_tar"):
			writeFakeBundleTar(t, filepath.Join(dir, "echo.tar.gz"), "0.2.6")
		case strings.Contains(joined, `make_script("start_clean"`):
			os.WriteFile(filepath.Join(dir, "start_clean.boot"), []byte("CLEAN"), 0o644)
		case strings.Contains(joined, "create_RELEASES"):
			os.MkdirAll(filepath.Join(dir, "releases"), 0o755)
			os.WriteFile(filepath.Join(dir, "releases", "RELEASES"), []byte("RELEASES"), 0o644)
		}
		return []byte("ok"), nil
	}
	t.Cleanup(func() { captureErl = orig })

	src := writeEchoAppSources(t)
	args := append([]string{"--out", out, "--vsn", "0.2.6", "--self-contained"}, src...)
	if err := releaseCmd(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatalf("releaseCmd: %v", err)
	}

	// Final artifact exists; unpack and assert the target-system layout.
	f, err := os.Open(filepath.Join(out, "echo-0.2.6.tar.gz"))
	if err != nil {
		t.Fatalf("final tarball missing: %v", err)
	}
	defer f.Close()
	got := t.TempDir()
	if err := release.Untar(f, got); err != nil {
		t.Fatal(err)
	}
	// The tarball unpacks into a self-named <app>-<vsn>/ directory.
	root := filepath.Join(got, "echo-0.2.6")
	for _, p := range []string{
		"bin/start", "bin/stop", "bin/erl",
		"releases/start_erl.data",
		"releases/0.2.6/start.boot",
		"releases/RELEASES",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("target system missing %s: %v", p, err)
		}
	}
}

// writeFakeBundleTar writes a .tar.gz mimicking systools:make_tar {erts} output:
// erts-*/, lib/, releases/<vsn>/echo.boot.
func writeFakeBundleTar(t *testing.T, path, vsn string) {
	t.Helper()
	stage := t.TempDir()
	os.MkdirAll(filepath.Join(stage, "erts-17.0.3", "bin"), 0o755)
	os.WriteFile(filepath.Join(stage, "erts-17.0.3", "bin", "erl"), []byte("x"), 0o755)
	os.MkdirAll(filepath.Join(stage, "lib", "echo-"+vsn, "ebin"), 0o755)
	os.MkdirAll(filepath.Join(stage, "releases", vsn), 0o755)
	os.WriteFile(filepath.Join(stage, "releases", vsn, "echo.boot"), []byte("BOOT"), 0o644)
	os.WriteFile(filepath.Join(stage, "releases", vsn, "echo.rel"), []byte("{release,...}."), 0o644)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := release.TarGz(stage, f); err != nil {
		t.Fatal(err)
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
