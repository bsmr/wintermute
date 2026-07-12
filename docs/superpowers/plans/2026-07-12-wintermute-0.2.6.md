# Wintermute 0.2.6 — self-contained OTP target system Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `wm release --self-contained` produces a standalone OTP target system
tarball that boots on a host with no Erlang installed via a generated,
self-locating `bin/start`.

**Architecture:** Extends the 0.2.5 `wm release` builder. When `--self-contained`
is set: the boot script is generated **non-local** (paths resolve from the
bundled `$ROOTDIR/lib` at boot), `sasl` is added to the `.rel`, and
`systools:make_tar` bundles the ERTS (`{erts, <OtpRoot>}`). The make_tar output is
then assembled into a full target system in Go (extract → add top-level `bin/`
launchers + `start_erl.data` + generated `bin/start`/`bin/stop` + `RELEASES` →
repack). The cookie comes from the OTP-default `~/.erlang.cookie` (no `-setcookie`,
no secret in the artifact). No transpiler change; `wm start` unchanged.

**Tech Stack:** Go stdlib only (incl. `archive/tar`, `compress/gzip`);
Erlang/OTP 29.0.3 `systools` + `release_handler` (via `erl`).

## Feasibility — already proven by spike (do not re-litigate)

A real-OTP spike (recorded in the spec) proved the load-bearing mechanics:
`make_tar {erts}` bundles a self-locating erts; a **non-local** boot is required;
a generated self-locating `bin/start` wrapper boots the unpacked bundle under a
**fully scrubbed environment** (`env -i PATH=/usr/bin:/bin`, no system Erlang, no
`-setcookie` → `~/.erlang.cookie`) and a scrubbed control node resolves
`{global, echo}` → `hello` (`WRAPPER REPLY=hello`). The plan builds exactly that
proven shape; Task 6 re-confirms it in-repo on real OTP.

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute`.
- **TDD, red→green:** write the failing test, watch it fail, then implement.
- **main()→run():** all logic in `internal/pkg/`; `main()` only calls `run()`.
- **Stdlib only.** No third-party modules.
- **Build output to `bin/`:** `go build -o bin/wm ./cmd/wm` — never bare `go build`.
- **No temp files in the project root.**
- **Code/comments/commits in English; replies to the user in German.**
- **Commits signed off** (`git commit -s`).
- **No transpiler change; `wm start` unchanged.** 0.2.6 is CLI/release tooling only.
- **CRITICAL — keep `local` for the non-self-contained path.** 0.2.5's `wm start`
  boots the metadata release in place against the *system* OTP, whose `$ROOTDIR`
  has no `<app>` in `lib/`; it relies on the `local` boot's absolute app-ebin path.
  Only `--self-contained` switches to a **non-local** boot (where `$ROOTDIR` is the
  bundle, which *does* contain `<app>` in `lib/`). Never make the 0.2.5 path non-local.
- **Cookie:** `bin/start` sets no `-setcookie` (uses `~/.erlang.cookie`). No cookie
  in the tarball, none on argv.
- **Existing seams (reuse):** `runErl`, `captureErl` (function vars in
  `internal/pkg/cli/`, overridden in tests). Existing helpers: `buildApp`,
  `parseStringFlag`, `parseVsnFlag`, `parseVersionFlag`, `parseOutFlag`,
  `readVersion`, `outPath`, `validNodeName`, `validAppName`; `erlang.Layout`
  (`Erl()`, `Erlc()`, `OtpLib()`, `ErtsVersion()`, `AppVersion(name)`);
  `release.RelResource/SysConfig/VmArgs/AppVsn/Manifest`.
- **OTP layout:** launchers to copy live in `Layout.OtpLib()/bin`
  (`erl`, `epmd`, `run_erl`, `to_erl`); erts version via `Layout.ErtsVersion()`.

---

### Task 1: `--self-contained` flag — non-local boot, sasl in `.rel`, `make_tar {erts}`

**Files:**
- Modify: `internal/pkg/cli/release.go`
- Test: `internal/pkg/cli/release_test.go`

**Interfaces:**
- Consumes: `Layout.AppVersion("sasl")`, `Layout.OtpLib()`, `captureErl`.
- Produces: `releaseCmd` accepts `--self-contained` (implies `--tar`); when set,
  the `.rel` includes `sasl`, `make_script` omits `local`, and `make_tar` includes
  `{erts, <OtpRoot>}`. (Assembly into a target system is Task 5.)

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/cli/release_test.go`:

```go
func TestReleaseSelfContainedRelAndEvals(t *testing.T) {
	stubErlc(t)
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

	home := t.TempDir()
	lib := filepath.Join(home, ".local", "erlang", "29.0.3", "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2", "lib/sasl-4.4"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestReleaseSelfContained -v`
Expected: FAIL — `--self-contained` unknown (falls into sources → buildApp error), and no sasl / local-still-present.

- [ ] **Step 3: Write minimal implementation**

In `releaseCmd`, extend the flag scan (next to `--tar`) and thread a
`selfContained` bool:

```go
	tar := false
	selfContained := false
	var filtered []string
	for _, a := range args {
		switch a {
		case "--tar":
			tar = true
		case "--self-contained":
			selfContained = true
			tar = true // --self-contained implies --tar
		default:
			filtered = append(filtered, a)
		}
	}
	args = filtered
```

Add `sasl` to the `.rel` apps when self-contained (after the `kernel`/`stdlib`
discovery, before `RelResource`):

```go
	apps := []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
	}
	if selfContained {
		saslV, err := l.AppVersion("sasl")
		if err != nil {
			return err
		}
		apps = append(apps, release.AppVsn{Name: "sasl", Vsn: saslV})
	}
	apps = append(apps, release.AppVsn{Name: appMod, Vsn: vsn})
	relBody := release.RelResource(appMod, vsn, erts, apps)
```

Make the `make_script` opts conditional (non-local when self-contained):

```go
	scriptOpts := `local,{path,["%s"]}`
	if selfContained {
		scriptOpts = `{path,["%s"]}` // non-local: paths resolve from $ROOTDIR/lib at boot
	}
	eval := fmt.Sprintf(
		`case systools:make_script("%s",[`+scriptOpts+`]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		appMod, absEbin)
	if res, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
		return fmt.Errorf("systools:make_script failed: %v\n%s", err, res)
	}
```

Make the `make_tar` opts bundle erts when self-contained:

```go
	if tar {
		tarOpts := `{path,["%s"]}`
		if selfContained {
			tarOpts = fmt.Sprintf(`{erts,%q},{path,["%%s"]}`, l.OtpLib())
		}
		eval := fmt.Sprintf(
			`case systools:make_tar("%s",[`+tarOpts+`]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
			appMod, absEbin)
		if res, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
			return fmt.Errorf("systools:make_tar failed: %v\n%s", err, res)
		}
	}
```

> Implementer note: rename the make_script block's `out`-shadowing variable to
> `res` while you are here (rolled-up 0.2.5 Minor). Update the usage string to add
> `[--self-contained]`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestRelease -v`
Expected: PASS (all release unit tests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/release.go internal/pkg/cli/release_test.go
git commit -s -m "feat(cli): wm release --self-contained — sasl, non-local boot, erts-bundled tar"
```

---

### Task 2: `release` package — target-system text helpers

**Files:**
- Modify: `internal/pkg/release/release.go`
- Test: `internal/pkg/release/release_test.go`

**Interfaces:**
- Produces: `func StartErlData(ertsVsn, relVsn string) string`;
  `func StartScript(vsn string) string`; `func StopScript(node, vsn string) string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/release/release_test.go`:

```go
func TestStartErlData(t *testing.T) {
	if got := StartErlData("17.0.3", "0.2.6"); got != "17.0.3 0.2.6\n" {
		t.Fatalf("StartErlData = %q", got)
	}
}

func TestStartScript(t *testing.T) {
	s := StartScript("0.2.6")
	for _, want := range []string{
		"#!/bin/sh",
		`HERE=$(cd "$(dirname "$0")/.." && pwd)`,
		`erts-*`,
		"-detached",
		"releases/0.2.6/start",
		"releases/0.2.6/sys.config",
		"releases/0.2.6/vm.args",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("StartScript missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "-setcookie") {
		t.Errorf("StartScript must not set a cookie (uses ~/.erlang.cookie):\n%s", s)
	}
}

func TestStopScript(t *testing.T) {
	s := StopScript("echo@127.0.0.1", "0.2.6")
	for _, want := range []string{
		"#!/bin/sh",
		"releases/0.2.6/start_clean",
		"'echo@127.0.0.1'",
		"init, stop",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("StopScript missing %q:\n%s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/release/ -run 'StartErlData|StartScript|StopScript' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/pkg/release/release.go`:

```go
// StartErlData is the releases/start_erl.data content: "<erts-vsn> <rel-vsn>".
func StartErlData(ertsVsn, relVsn string) string {
	return fmt.Sprintf("%s %s\n", ertsVsn, relVsn)
}

// StartScript is a self-locating bin/start: it computes the target root from its
// own path, finds the bundled erts, and boots the release detached. No -setcookie
// (erl uses ~/.erlang.cookie), so the tarball carries no secret.
func StartScript(vsn string) string {
	return `#!/bin/sh
HERE=$(cd "$(dirname "$0")/.." && pwd)
ERTS=$(basename "$HERE"/erts-*)
exec "$HERE/$ERTS/bin/erl" -detached -boot "$HERE/releases/` + vsn + `/start" \
  -config "$HERE/releases/` + vsn + `/sys.config" \
  -args_file "$HERE/releases/` + vsn + `/vm.args"
`
}

// StopScript is a self-locating bin/stop: a short-lived control node (booted with
// the bundled start_clean) that rpc-stops the release node. It shares the node's
// ~/.erlang.cookie automatically on the same host.
func StopScript(node, vsn string) string {
	return `#!/bin/sh
HERE=$(cd "$(dirname "$0")/.." && pwd)
ERTS=$(basename "$HERE"/erts-*)
exec "$HERE/$ERTS/bin/erl" -boot "$HERE/releases/` + vsn + `/start_clean" \
  -name "wmstop_$$@127.0.0.1" -noshell \
  -eval "rpc:call('` + node + `', init, stop, []), init:stop()."
`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/release/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/release/release.go internal/pkg/release/release_test.go
git commit -s -m "feat(release): target-system launcher/start_erl.data text helpers"
```

---

### Task 3: `release` package — tar extract/repack helpers

**Files:**
- Create: `internal/pkg/release/archive.go`
- Test: `internal/pkg/release/archive_test.go`

**Interfaces:**
- Produces: `func Untar(r io.Reader, dst string) error` (reads a .tar.gz);
  `func TarGz(srcDir string, w io.Writer) error` (writes a .tar.gz of srcDir's
  contents, preserving file modes so `bin/start` stays executable).

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/release/archive_test.go`:

```go
package release

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzUntarRoundTrip(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0o755)
	os.WriteFile(filepath.Join(src, "bin", "start"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(src, "note.txt"), []byte("hi"), 0o644)

	var buf bytes.Buffer
	if err := TarGz(src, &buf); err != nil {
		t.Fatalf("TarGz: %v", err)
	}
	dst := t.TempDir()
	if err := Untar(&buf, dst); err != nil {
		t.Fatalf("Untar: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dst, "bin", "start"))
	if err != nil {
		t.Fatalf("start missing after round-trip: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("start lost its executable bit: %o", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(filepath.Join(dst, "note.txt"))
	if string(b) != "hi" {
		t.Errorf("note.txt content = %q", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/release/ -run RoundTrip -v`
Expected: FAIL — `TarGz`/`Untar` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/pkg/release/archive.go`:

```go
package release

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Untar extracts a gzipped tar stream into dst, preserving file modes.
func Untar(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.Clean("/"+hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // trusted systools archive
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
}

// TarGz writes a gzipped tar of srcDir's contents (paths relative to srcDir) to w,
// preserving file modes.
func TarGz(srcDir string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(srcDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		link := ""
		if fi.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		hdr.Name = strings.ReplaceAll(rel, string(os.PathSeparator), "/")
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/release/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/release/archive.go internal/pkg/release/archive_test.go
git commit -s -m "feat(release): tar.gz extract/repack helpers (mode-preserving)"
```

---

### Task 4: `release` package — write the launcher layout into an unpacked tree

**Files:**
- Create: `internal/pkg/release/target.go`
- Test: `internal/pkg/release/target_test.go`

**Interfaces:**
- Consumes: `StartScript`, `StopScript`, `StartErlData` (Task 2).
- Produces: `func WriteLauncherLayout(root, app, node, vsn, ertsVsn, otpBinDir string) error`
  — into an already-unpacked target root (which has `erts-*/`, `lib/`,
  `releases/<vsn>/`): create top-level `bin/` copying `otpBinDir/{erl,epmd,run_erl,
  to_erl}`, write `bin/start` (0o755) and `bin/stop` (0o755), write
  `releases/start_erl.data`, and copy `releases/<vsn>/<app>.boot` →
  `releases/<vsn>/start.boot`.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/release/target_test.go`:

```go
package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLauncherLayout(t *testing.T) {
	root := t.TempDir()
	// Fake unpacked bundle bits the layout depends on.
	os.MkdirAll(filepath.Join(root, "releases", "0.2.6"), 0o755)
	os.WriteFile(filepath.Join(root, "releases", "0.2.6", "echo.boot"), []byte("BOOT"), 0o644)
	// Fake OTP bin/ source.
	otpBin := t.TempDir()
	for _, f := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		os.WriteFile(filepath.Join(otpBin, f), []byte("launcher"), 0o755)
	}

	if err := WriteLauncherLayout(root, "echo", "echo@127.0.0.1", "0.2.6", "17.0.3", otpBin); err != nil {
		t.Fatalf("WriteLauncherLayout: %v", err)
	}

	// start/stop present and executable.
	for _, f := range []string{"bin/start", "bin/stop", "bin/erl"} {
		fi, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s not executable: %o", f, fi.Mode().Perm())
		}
	}
	// start_erl.data + start.boot.
	sed, _ := os.ReadFile(filepath.Join(root, "releases", "start_erl.data"))
	if string(sed) != "17.0.3 0.2.6\n" {
		t.Errorf("start_erl.data = %q", sed)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "0.2.6", "start.boot")); err != nil {
		t.Errorf("start.boot not created: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/release/ -run WriteLauncherLayout -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/pkg/release/target.go`:

```go
package release

import (
	"os"
	"path/filepath"
)

// WriteLauncherLayout turns an unpacked make_tar bundle (erts-*/, lib/,
// releases/<vsn>/) into a full target system: a top-level bin/ with the erts
// launchers plus a self-locating bin/start and bin/stop, releases/start_erl.data,
// and releases/<vsn>/start.boot (a copy of <app>.boot).
func WriteLauncherLayout(root, app, node, vsn, ertsVsn, otpBinDir string) error {
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		data, err := os.ReadFile(filepath.Join(otpBinDir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(binDir, name), data, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, "start"), []byte(StartScript(vsn)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(binDir, "stop"), []byte(StopScript(node, vsn)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "releases", "start_erl.data"),
		[]byte(StartErlData(ertsVsn, vsn)), 0o644); err != nil {
		return err
	}
	boot, err := os.ReadFile(filepath.Join(root, "releases", vsn, app+".boot"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "releases", vsn, "start.boot"), boot, 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/release/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/release/target.go internal/pkg/release/target_test.go
git commit -s -m "feat(release): assemble target-system launcher layout"
```

---

### Task 5: `wm release --self-contained` — assemble + repack the target system

**Files:**
- Modify: `internal/pkg/cli/release.go`
- Test: `internal/pkg/cli/release_test.go`

**Interfaces:**
- Consumes: `release.Untar/TarGz/WriteLauncherLayout/RelResource`, `captureErl`,
  `Layout.OtpLib/ErtsVersion/AppVersion`.
- Produces: after `make_tar {erts}`, `releaseCmd` (when `selfContained`) unpacks
  `<app>.tar.gz` into a work dir, generates `start_clean.boot` + `RELEASES` via
  `erl`, writes the launcher layout, and repacks `<out>/<app>-<vsn>.tar.gz`.

- [ ] **Step 1: Write the failing test**

The test drives `releaseCmd --self-contained` with a `captureErl` stub that
*materialises* the make_tar output (a real .tar.gz on disk with the bundle bits),
so the Go assembly runs against a realistic tree without a BEAM. Add to
`internal/pkg/cli/release_test.go`:

```go
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
	// make_script for start_clean, write a fake start_clean.boot; RELEASES: no-op.
	orig := captureErl
	captureErl = func(_ context.Context, dir, _ string, a ...string) ([]byte, error) {
		joined := strings.Join(a, " ")
		switch {
		case strings.Contains(joined, "make_tar"):
			writeFakeBundleTar(t, filepath.Join(dir, "echo.tar.gz"), "0.2.6")
		case strings.Contains(joined, `make_script("start_clean"`):
			os.WriteFile(filepath.Join(dir, "start_clean.boot"), []byte("CLEAN"), 0o644)
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
	for _, p := range []string{
		"bin/start", "bin/stop", "bin/erl",
		"releases/start_erl.data",
		"releases/0.2.6/start.boot",
		"releases/RELEASES",
	} {
		if _, err := os.Stat(filepath.Join(got, p)); err != nil {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestReleaseSelfContainedAssembles -v`
Expected: FAIL — no `echo-0.2.6.tar.gz` / no assembly yet.

- [ ] **Step 3: Write minimal implementation**

In `releaseCmd`, after the `if tar { … }` make_tar block, add the assembly when
`selfContained`:

```go
	if selfContained {
		if err := assembleTargetSystem(ctx, l, out, relDir, appMod, name, vsn); err != nil {
			return err
		}
	}
```

Add the helper to `release.go`:

```go
// assembleTargetSystem turns the make_tar {erts} output into a full standalone
// target system: unpack -> generate start_clean.boot + RELEASES via erl -> write
// the launcher layout -> repack <out>/<app>-<vsn>.tar.gz.
func assembleTargetSystem(ctx context.Context, l erlang.Layout, out, relDir, appMod, node, vsn string) error {
	work := filepath.Join(out, ".target")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// Unpack the make_tar output (written as <relDir>/<app>.tar.gz).
	tf, err := os.Open(filepath.Join(relDir, appMod+".tar.gz"))
	if err != nil {
		return err
	}
	err = release.Untar(tf, work)
	tf.Close()
	if err != nil {
		return err
	}

	workRel := filepath.Join(work, "releases", vsn)

	// start_clean.boot (kernel+stdlib) for control nodes (bin/stop), non-local.
	erts, err := l.ErtsVersion()
	if err != nil {
		return err
	}
	kernel, err := l.AppVersion("kernel")
	if err != nil {
		return err
	}
	stdlib, err := l.AppVersion("stdlib")
	if err != nil {
		return err
	}
	cleanRel := release.RelResource("start_clean", vsn, erts, []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
	})
	if err := os.WriteFile(filepath.Join(workRel, "start_clean.rel"), []byte(cleanRel), 0o644); err != nil {
		return err
	}
	cleanEval := `case systools:make_script("start_clean",[{path,["../../lib/*/ebin"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`
	if res, err := captureErl(ctx, workRel, l.Erl(), "-noshell", "-eval", cleanEval); err != nil {
		return fmt.Errorf("make_script start_clean failed: %v\n%s", err, res)
	}

	// RELEASES (release_handler), for the canonical target-system layout.
	absWork, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	relEval := fmt.Sprintf(
		`case release_handler:create_RELEASES(%q, %q) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		absWork, filepath.Join(absWork, "releases", vsn, appMod+".rel"))
	if res, err := captureErl(ctx, work, l.Erl(), "-noshell", "-eval", relEval); err != nil {
		return fmt.Errorf("create_RELEASES failed: %v\n%s", err, res)
	}

	// Launcher layout: bin/, start_erl.data, start.boot.
	if err := release.WriteLauncherLayout(work, appMod, node, vsn, erts, filepath.Join(l.OtpLib(), "bin")); err != nil {
		return err
	}

	// Repack the final self-contained artifact.
	final, err := os.Create(filepath.Join(out, appMod+"-"+vsn+".tar.gz"))
	if err != nil {
		return err
	}
	defer final.Close()
	return release.TarGz(work, final)
}
```

> Implementer note: the `../../lib/*/ebin` relative path in `cleanEval` runs with
> cwd = `workRel` (`.target/releases/<vsn>`), so `../../lib` is the unpacked lib.
> Task 6 (real OTP) confirms `start_clean` + `create_RELEASES` succeed; if
> `create_RELEASES` needs the LibDirs argument or the `{path,…}` glob needs
> concrete dirs, adjust there (the unit test stubs both erl calls).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestRelease -v` and `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/release.go internal/pkg/cli/release_test.go
git commit -s -m "feat(cli): assemble + repack self-contained target-system tarball"
```

---

### Task 6: Rung VII — self-contained target system boots scrubbed (integration)

**Files:**
- Create: `internal/pkg/cli/selfcontained_integration_test.go`

**Interfaces:**
- Consumes: the real `Run(ctx, args, …)` entry point on real OTP 29.0.3.
- Produces: `TestSelfContainedTargetSystemEndToEnd` — the real-OTP regression guard.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/cli/selfcontained_integration_test.go` (`//go:build integration`).
It runs the real `wm release --self-contained`, unpacks the artifact into a fresh
dir, boots `bin/start` under a **scrubbed environment**, and drives a scrubbed
control node to call `{global, echo}` — mirroring the proven spike.

```go
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
	"time"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
)

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

	// 2. Unpack the artifact into a fresh HOME (isolated ~/.erlang.cookie).
	target := t.TempDir()
	f, err := os.Open(filepath.Join(out, "echoapp-"+vsn+".tar.gz"))
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if err := release.Untar(f, target); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// Pin a unique node name (rewrite the bundled vm.args) to avoid epmd clashes.
	node := fmt.Sprintf("wmsc_%d@127.0.0.1", os.Getpid())
	os.WriteFile(filepath.Join(target, "releases", vsn, "vm.args"), []byte("-name "+node+"\n"), 0o644)

	scrub := []string{"PATH=/usr/bin:/bin", "HOME=" + target}
	erlBin := filepath.Join(target, "erts-"+mustErts(t, target), "bin", "erl")

	// 3. Boot bin/start under a SCRUBBED environment (no system Erlang).
	start := exec.CommandContext(ctx, filepath.Join(target, "bin", "start"))
	start.Env = scrub
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("bin/start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		_ = stop.Run()
	})

	// 4. Scrubbed control node resolves {global, echo} and calls.
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
	_ = time.Second
}

func mustErts(t *testing.T, root string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(root, "erts-*"))
	if len(m) != 1 {
		t.Fatalf("expected one erts-* in bundle, got %v", m)
	}
	return strings.TrimPrefix(filepath.Base(m[0]), "erts-")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags integration ./internal/pkg/cli/ -run TestSelfContained -v`
Expected: FAIL initially — iterate against real OTP until green (this is where the
`start_clean`/`create_RELEASES` erl calls and the scrubbed boot are confirmed).

- [ ] **Step 3: Make it pass**

Iterate on real OTP: confirm `make_script("start_clean", …)` resolves kernel/stdlib
(add concrete lib ebin paths to its `{path,…}` if the glob fails), confirm
`create_RELEASES` succeeds, confirm `bin/start` boots scrubbed. Mirror any fix back
into `assembleTargetSystem`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags integration ./internal/pkg/cli/ -run TestSelfContained -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/selfcontained_integration_test.go internal/pkg/cli/release.go
git commit -s -m "test(cli): rung VII — self-contained target system boots scrubbed"
```

---

### Task 7: Docs, verification gate, handover

**Files:**
- Modify: `internal/pkg/cli/cli.go` (usage/commands help), `README.md`,
  `HANDOVER.md`, `docs/SDK-INDEX.md`.

**Interfaces:** none.

- [ ] **Step 1: Usage + README**

Add `--self-contained` to the `wm release` usage string and document the
standalone flow in `README.md` (unpack `<app>-<vsn>.tar.gz`, run `./bin/start` on
a host with no Erlang). Add a `TestUsage`-style assertion if usage text is tested.

Run: `go test ./internal/pkg/cli/ -run TestUsage -v` (if present) → PASS.

- [ ] **Step 2: Full unit + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: green (`cli`, `erlang`, `release`, `transpile`, `pkg/otp`).

- [ ] **Step 3: Real integration**

Run: `go test -tags integration ./internal/pkg/ladder/` and
`go test -tags integration ./internal/pkg/cli/`.
Expected: all prior rungs + the new self-contained e2e PASS on OTP 29.0.3.

- [ ] **Step 4: Security scans**

Run: `govulncheck ./...`; `gosec ./...`; `gitleaks detect`.
Expected: no new vulnerability class. Triage any new `G204`/`G304`/`G301`/`G306` in
`release.go`/`archive.go`/`target.go` as the accepted dual-use CLI class (the
tarball's `0o755` launchers and `0o644` resources are intentional; no secret is
written). Document the delta in HANDOVER as prior cycles did.

- [ ] **Step 5: SDK index + handover, commit**

Regenerate `/sdk-index` (no `pkg/` change expected → likely unchanged). Rewrite
`HANDOVER.md` for 0.2.6: delivered summary, verification evidence, gosec delta,
and the merge/push instructions (origin→upstream→github, Copilot gate first).

```bash
git add internal/pkg/cli/cli.go README.md HANDOVER.md docs/SDK-INDEX.md
git commit -s -m "docs: 0.2.6 verification gate + handover (self-contained target system)"
```

---

## Self-Review

**Spec coverage:**
- `--self-contained` flag → Task 1. ✓
- sasl in `.rel` + non-local boot + `make_tar {erts}` → Task 1. ✓
- Target-system layout (bin/, start_erl.data, start.boot, RELEASES) → Tasks 2, 4, 5. ✓
- Generated self-locating `bin/start`/`bin/stop` → Task 2, wired Task 4. ✓
- Cookie via `~/.erlang.cookie` (no `-setcookie`) → Task 2 (`StartScript`), asserted. ✓
- tar extract/repack → Task 3. ✓
- start_clean.boot + RELEASES via erl → Task 5. ✓
- Scrubbed-env verification (rung VII) → Task 6. ✓
- No transpiler change; `wm start` unchanged → confirmed (no task touches transpile or startCmd). ✓
- Docs + gate → Task 7. ✓

**Placeholder scan:** the two "Implementer note" blocks point at concrete real-OTP
confirmations (Task 5's erl-call details, Task 6's iteration) — legitimate, since
`create_RELEASES`/`start_clean` exact options must be confirmed against the live
toolchain, not fabricated. The spike already proved the boot shape.

**Type consistency:** `WriteLauncherLayout(root, app, node, vsn, ertsVsn, otpBinDir)`
(Task 4) ↔ called in Task 5; `StartScript(vsn)`/`StopScript(node, vsn)`/
`StartErlData(ertsVsn, relVsn)` (Task 2) ↔ used in Task 4; `Untar`/`TarGz` (Task 3)
↔ used in Task 5 and the tests; `selfContained` bool threads through Task 1 → Task 5.
Consistent.
