# Wintermute 0.2.5 — full OTP release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package the transpiled OTP application as a formal OTP release
(`releases/<vsn>/` with `.rel`, boot script, `sys.config`, `vm.args`, plus a
`lib/<app>-<vsn>/ebin` layout) and boot it via `erl -boot`, replacing 0.2.4's
ad-hoc `-eval application:start`.

**Architecture:** Two new CLI verbs' worth of behaviour: `wm release <sources>`
builds the release (new `internal/pkg/release` package holds the pure builders;
`systools:make_script`/`make_tar` invoked via the existing `erl` seams);
`wm start <release-dir>` boots a finished release, generating the Erlang cookie
into a `0o600` run-file consumed via `-args_file` (never on argv, never in the
release). No transpiler change.

**Tech Stack:** Go stdlib only; Erlang/OTP 29.0.3 `systools` (via `erl`).

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute` (not the GitHub path).
- **TDD, red→green:** write the failing test, watch it fail, then implement.
- **main()→run():** all logic in `internal/pkg/`; `main()` only calls `run()`.
- **Stdlib only.** No third-party modules.
- **Build output to `bin/`:** `go build -o bin/wm ./cmd/wm` — never bare `go build`.
- **No temp files in the project root.**
- **Erlang correctness over Go idiom:** no hidden automatisms; reject, never auto-fix.
- **Code/comments/commits in English; replies to the user in German.**
- **OTP install layout:** the OTP tree lives under `Layout.Root/lib/erlang/`:
  `erts-<v>` directly there, `lib/<app>-<v>` for kernel/stdlib.
- **Existing seams (reuse, do not re-invent):** `runErl Runner = execRunner`,
  `captureErl`, `attachErl` (function vars in `internal/pkg/cli/`, overridden in
  tests to assert command lines without a real BEAM).
- **Existing helpers to reuse:** `buildApp(paths []string, out string) (appMod string,
  modules, registered []string, err error)`, `transpile.AppResource(app, vsn string,
  modules, registered []string) string`, `erlang.NewLayout(home, version string)
  Layout`, `erlang.ValidateVersion`, `parseStringFlag`, `parseVsnFlag`,
  `parseVersionFlag`, `parseOutFlag`, `readVersion`, `outPath(dir, mod)`,
  `validNodeName`, `validAtom`, `validAppName`, `newCookie`, `writeState`,
  `readState`, `removeState`, `stateDir`, `statePath`, `NodeState{Node, Cookie, CodePath}`.

---

### Task 1: OTP version discovery on `erlang.Layout`

**Files:**
- Modify: `internal/pkg/erlang/paths.go`
- Test: `internal/pkg/erlang/paths_test.go`

**Interfaces:**
- Produces: `func (l Layout) OtpLib() string` (returns `Root/lib/erlang`);
  `func (l Layout) ErtsVersion() (string, error)`;
  `func (l Layout) AppVersion(name string) (string, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/erlang/paths_test.go` (create if absent, `package erlang`):

```go
package erlang

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeOtp(t *testing.T) Layout {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Layout{Root: root, Bin: filepath.Join(root, "bin")}
}

func TestErtsVersion(t *testing.T) {
	l := fakeOtp(t)
	got, err := l.ErtsVersion()
	if err != nil || got != "17.0.3" {
		t.Fatalf("ErtsVersion() = %q, %v; want 17.0.3", got, err)
	}
}

func TestAppVersion(t *testing.T) {
	l := fakeOtp(t)
	for name, want := range map[string]string{"kernel": "11.0.3", "stdlib": "8.0.2"} {
		got, err := l.AppVersion(name)
		if err != nil || got != want {
			t.Fatalf("AppVersion(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestAppVersionMissing(t *testing.T) {
	l := fakeOtp(t)
	if _, err := l.AppVersion("nosuch"); err == nil {
		t.Fatal("AppVersion(nosuch) should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run 'ErtsVersion|AppVersion' -v`
Expected: FAIL — `l.ErtsVersion undefined` / `l.AppVersion undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/pkg/erlang/paths.go`:

```go
import (
	"fmt"
	"path/filepath"
	"strings"
)

// OtpLib is the OTP tree root: $PREFIX/lib/erlang, holding erts-<v> and lib/.
func (l Layout) OtpLib() string { return filepath.Join(l.Root, "lib", "erlang") }

// globVersion returns the single version suffix of dir/<prefix>-*, erroring on
// zero or multiple matches (an ambiguous install is a real problem, not a guess).
func globVersion(dir, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"-*"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one %s-* under %s, found %d", prefix, dir, len(matches))
	}
	return strings.TrimPrefix(filepath.Base(matches[0]), prefix+"-"), nil
}

// ErtsVersion reads the erts version from OtpLib/erts-<v>.
func (l Layout) ErtsVersion() (string, error) { return globVersion(l.OtpLib(), "erts") }

// AppVersion reads an OTP application's version from OtpLib/lib/<name>-<v>.
func (l Layout) AppVersion(name string) (string, error) {
	return globVersion(filepath.Join(l.OtpLib(), "lib"), name)
}
```

(Keep the existing `import "path/filepath"` — merge into the import block above.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/erlang/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/paths.go internal/pkg/erlang/paths_test.go
git commit -s -m "feat(erlang): OTP erts/app version discovery on Layout"
```

---

### Task 2: `release` package — `.rel`, `sys.config`, `vm.args` builders

**Files:**
- Create: `internal/pkg/release/release.go`
- Test: `internal/pkg/release/release_test.go`

**Interfaces:**
- Produces: `type AppVsn struct { Name, Vsn string }`;
  `func RelResource(name, vsn, erts string, apps []AppVsn) string`;
  `func SysConfig(app string) string`;
  `func VmArgs(node string) string`.

- [ ] **Step 1: Write the failing test**

```go
package release

import "testing"

func TestRelResource(t *testing.T) {
	got := RelResource("echo", "0.2.5", "17.0.3", []AppVsn{
		{"kernel", "11.0.3"}, {"stdlib", "8.0.2"}, {"echo", "0.2.5"},
	})
	want := `{release, {"echo", "0.2.5"},
 {erts, "17.0.3"},
 [{kernel, "11.0.3"},
  {stdlib, "8.0.2"},
  {echo, "0.2.5"}]}.
`
	if got != want {
		t.Fatalf("RelResource mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestSysConfig(t *testing.T) {
	if got := SysConfig("echo"); got != "[{echo, []}].\n" {
		t.Fatalf("SysConfig = %q", got)
	}
}

func TestVmArgs(t *testing.T) {
	if got := VmArgs("echo@127.0.0.1"); got != "-name echo@127.0.0.1\n" {
		t.Fatalf("VmArgs = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/release/ -v`
Expected: FAIL — package/functions undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package release builds OTP release resources (.rel, sys.config, vm.args) and
// the wm.json manifest. All builders are pure string/JSON functions; the CLI
// wires them and invokes systools via the erl seam.
package release

import (
	"fmt"
	"strings"
)

// AppVsn names an application and its version for the .rel apps list.
type AppVsn struct{ Name, Vsn string }

// RelResource builds an OTP release resource file (.rel) term.
func RelResource(name, vsn, erts string, apps []AppVsn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{release, {%q, %q},\n", name, vsn)
	fmt.Fprintf(&b, " {erts, %q},\n", erts)
	b.WriteString(" [")
	for i, a := range apps {
		if i > 0 {
			b.WriteString("\n  ")
		}
		fmt.Fprintf(&b, "{%s, %q}", a.Name, a.Vsn)
		if i < len(apps)-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("]}.\n")
	return b.String()
}

// SysConfig builds an empty-but-valid sys.config scaffold for app.
func SysConfig(app string) string { return fmt.Sprintf("[{%s, []}].\n", app) }

// VmArgs builds a vm.args carrying only the node name — no cookie (the cookie
// is supplied at boot via a separate 0o600 -args_file overlay).
func VmArgs(node string) string { return fmt.Sprintf("-name %s\n", node) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/release/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/release/release.go internal/pkg/release/release_test.go
git commit -s -m "feat(release): .rel/sys.config/vm.args builders"
```

---

### Task 3: `release` package — `Manifest` (`wm.json`)

**Files:**
- Modify: `internal/pkg/release/release.go`
- Test: `internal/pkg/release/release_test.go`

**Interfaces:**
- Produces: `type Manifest struct { App, Vsn, Node string }` (JSON tags
  `app`/`vsn`/`node`); `func (m Manifest) Marshal() ([]byte, error)`;
  `func ParseManifest(data []byte) (Manifest, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/release/release_test.go`:

```go
import "testing"

func TestManifestRoundTrip(t *testing.T) {
	m := Manifest{App: "echo", Vsn: "0.2.5", Node: "echo@127.0.0.1"}
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(data)
	if err != nil || got != m {
		t.Fatalf("round-trip = %+v, %v; want %+v", got, err, m)
	}
}

func TestParseManifestBad(t *testing.T) {
	if _, err := ParseManifest([]byte("{not json")); err == nil {
		t.Fatal("ParseManifest should error on bad JSON")
	}
}
```

(Merge the `import "testing"` into the file's existing import — do not duplicate.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/release/ -run Manifest -v`
Expected: FAIL — `Manifest`/`ParseManifest` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/pkg/release/release.go` (add `"encoding/json"` to imports):

```go
// Manifest is the wm.json at a release root: the single source of truth wm start
// reads back to recover app, vsn, and node without parsing vm.args or globbing.
type Manifest struct {
	App  string `json:"app"`
	Vsn  string `json:"vsn"`
	Node string `json:"node"`
}

// Marshal renders the manifest as indented JSON.
func (m Manifest) Marshal() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

// ParseManifest decodes a wm.json manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	err := json.Unmarshal(data, &m)
	return m, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/release/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/release/release.go internal/pkg/release/release_test.go
git commit -s -m "feat(release): wm.json manifest marshal/parse"
```

---

### Task 4: `wm release` — build the on-disk release tree (no systools yet)

**Files:**
- Modify: `internal/pkg/cli/cli.go`
- Create: `internal/pkg/cli/release.go`
- Test: `internal/pkg/cli/release_test.go`

**Interfaces:**
- Consumes: `buildApp`, `transpile.AppResource`, `release.RelResource/SysConfig/
  VmArgs/Manifest/AppVsn`, `erlang.NewLayout/.ErtsVersion/.AppVersion/.Erlc`,
  `runErl`, `parseStringFlag/parseVsnFlag/parseVersionFlag/parseOutFlag`,
  `readVersion`, `outPath`, `validNodeName`.
- Produces: `func releaseCmd(ctx context.Context, args []string, stdout io.Writer) error`;
  release layout `<out>/{wm.json, lib/<app>-<vsn>/ebin/{<app>.app,*.beam},
  releases/<vsn>/{<app>.rel,sys.config,vm.args}}`.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/cli/release_test.go`:

```go
package cli

import (
	"bytes"
	"context"
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
		!strings.Contains(string(rel), `{echo, "0.2.5"}`) {
		t.Errorf("echo.rel missing discovered versions:\n%s", rel)
	}
	vm, _ := os.ReadFile(filepath.Join(out, "releases/0.2.5/vm.args"))
	if strings.Contains(string(vm), "setcookie") {
		t.Errorf("vm.args must not carry a cookie:\n%s", vm)
	}
}
```

Add the source helper at the bottom of `release_test.go`. Reuse the same minimal
echo app the existing tests transpile — mirror what `writeEchoAppSources` must
produce: an application module. If an equivalent helper already exists in
`cli_test.go`, call it instead of redefining; otherwise add:

```go
func writeEchoAppSources(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "echo.go")
	// Minimal application module: package echo with an application behaviour.
	// Copy the smallest application fixture the transpiler accepts (see
	// testdata/persistent/go/echoapp/main.go for the shape).
	body := mustReadFixtureApp(t) // load testdata/persistent/go/echoapp/main.go
	if err := os.WriteFile(app, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{app}
}
```

> Implementer note: the exact minimal application source is whatever
> `transpile.Module` reports `Behaviour == "application"` for. Read
> `testdata/persistent/go/echoapp/main.go` and reuse its content via a small
> `mustReadFixtureApp` that reads that file. Do NOT invent new Go syntax; the
> transpiler only accepts the echo subset.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestReleaseBuildsTree -v`
Expected: FAIL — `releaseCmd` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/pkg/cli/release.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// releaseCmd builds a formal OTP release tree from Go sources. It does not boot;
// `wm start <dir>` boots the result.
func releaseCmd(ctx context.Context, args []string, stdout io.Writer) error {
	name, rest, err := parseStringFlag(args, "--name", "")
	if err != nil {
		return err
	}
	if name != "" && !validNodeName(name) {
		return fmt.Errorf("invalid node name %q (must match name@host)", name)
	}
	version, rest, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	vsn, rest, err := parseVsnFlag(rest)
	if err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wm release <path>... [--name N] [--out DIR] [--version X] [--vsn V] [--tar]")
	}
	if vsn == "" {
		if v, verr := readVersion(); verr == nil {
			vsn = v
		} else {
			vsn = "0.0.0"
		}
	}

	// Transpile to a staging dir first so we learn the app module name before we
	// can name the versioned lib/<app>-<vsn>/ebin directory.
	stage := filepath.Join(out, ".stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	appMod, modules, registered, err := buildApp(rest, stage)
	if err != nil {
		return err
	}
	if appMod == "" {
		return fmt.Errorf("no application module among %v; wm release needs one", rest)
	}

	ebin := filepath.Join(out, "lib", appMod+"-"+vsn, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	for _, m := range modules {
		if err := runErl(ctx, ".", l.Erlc(), "-o", ebin, outPath(stage, m)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(ebin, appMod+".app"),
		[]byte(transpile.AppResource(appMod, vsn, modules, registered)), 0o644); err != nil {
		return err
	}
	_ = os.RemoveAll(stage)

	relDir := filepath.Join(out, "releases", vsn)
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		return err
	}
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
	relBody := release.RelResource(appMod, vsn, erts, []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
		{Name: appMod, Vsn: vsn},
	})
	if err := os.WriteFile(filepath.Join(relDir, appMod+".rel"), []byte(relBody), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(relDir, "sys.config"), []byte(release.SysConfig(appMod)), 0o644); err != nil {
		return err
	}
	if name == "" {
		name = appMod + "@127.0.0.1"
	}
	if !validNodeName(name) {
		return fmt.Errorf("invalid node name %q (must match name@host)", name)
	}
	if err := os.WriteFile(filepath.Join(relDir, "vm.args"), []byte(release.VmArgs(name)), 0o644); err != nil {
		return err
	}
	man, err := release.Manifest{App: appMod, Vsn: vsn, Node: name}.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "wm.json"), man, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "built release %s %s (%s)\n", appMod, vsn, name)
	return nil
}
```

Register the subcommand in `internal/pkg/cli/cli.go` `Run` dispatch (next to `build`):

```go
	case "release":
		return releaseCmd(ctx, args[1:], stdout)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestReleaseBuildsTree -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/release.go internal/pkg/cli/release_test.go internal/pkg/cli/cli.go
git commit -s -m "feat(cli): wm release builds the OTP release tree"
```

---

### Task 5: `wm release` — generate the boot script via `systools:make_script`

**Files:**
- Modify: `internal/pkg/cli/release.go`
- Test: `internal/pkg/cli/release_test.go`

**Interfaces:**
- Consumes: `captureErl` seam, the release layout from Task 4.
- Produces: a `captureErl` call running `systools:make_script` with cwd
  `releases/<vsn>` and the app ebin on the path; surfaces systools error tuples.

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/cli/release_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestReleaseInvokesMakeScript -v`
Expected: FAIL — `captureErl` never called / gotEval empty.

- [ ] **Step 3: Write minimal implementation**

In `release.go`, after writing `sys.config`/`vm.args`/`wm.json` (before the final
`Fprintf`), add the boot-script generation. Insert once `ebin` and `relDir` and
`appMod` are known:

```go
	// Generate the boot script with systools. cwd = releases/<vsn> so make_script
	// reads <app>.rel and writes <app>.script/<app>.boot there. `local` bakes the
	// build-time ebin paths so the release boots in place without an install step.
	absEbin, err := filepath.Abs(ebin)
	if err != nil {
		return err
	}
	eval := fmt.Sprintf(
		`case systools:make_script("%s",[local,{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		appMod, absEbin)
	if out, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
		return fmt.Errorf("systools:make_script failed: %v\n%s", err, out)
	}
```

> Implementer note: if the integration rung (Task 9) shows make_script cannot
> find kernel/stdlib `.app`, extend `{path,[...]}` with their ebin dirs, built
> from `filepath.Join(l.OtpLib(), "lib", "kernel-"+kernel, "ebin")` and the
> stdlib equivalent. Start with the app ebin only; let real OTP decide.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestRelease -v`
Expected: PASS (both release tests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/release.go internal/pkg/cli/release_test.go
git commit -s -m "feat(cli): wm release generates the boot script via systools"
```

---

### Task 6: `wm release --tar` — release tarball via `systools:make_tar`

**Files:**
- Modify: `internal/pkg/cli/release.go`
- Test: `internal/pkg/cli/release_test.go`

**Interfaces:**
- Consumes: `parseStringFlag`/manual flag scan for `--tar`, `captureErl`.
- Produces: with `--tar`, a second `captureErl` running `systools:make_tar`
  (no `{erts,_}` — runs against an installed same-version OTP).

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/cli/release_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestReleaseTar -v`
Expected: FAIL — no make_tar eval.

- [ ] **Step 3: Write minimal implementation**

At the top of `releaseCmd`, extract a boolean `--tar` flag from `args` before the
other parsing (a simple scan; `--tar` takes no value):

```go
	tar := false
	var filtered []string
	for _, a := range args {
		if a == "--tar" {
			tar = true
			continue
		}
		filtered = append(filtered, a)
	}
	args = filtered
```

Then after the make_script block:

```go
	if tar {
		eval := fmt.Sprintf(
			`case systools:make_tar("%s",[{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
			appMod, absEbin)
		if out, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
			return fmt.Errorf("systools:make_tar failed: %v\n%s", err, out)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestRelease -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/release.go internal/pkg/cli/release_test.go
git commit -s -m "feat(cli): wm release --tar packages a release tarball"
```

---

### Task 7: `wm start <release-dir>` — boot a finished release (breaking change)

**Files:**
- Modify: `internal/pkg/cli/cli.go` (replace `startCmd` body)
- Modify: `internal/pkg/cli/cli_test.go` (existing start tests → new interface)
- Test: `internal/pkg/cli/cli_test.go` (new run-file + argv assertions)

**Interfaces:**
- Consumes: `release.ParseManifest`, `newCookie`, `stateDir`, `writeState`,
  `runErl`, `erlang.NewLayout`, `erlang.ValidateVersion`, `parseVersionFlag`.
- Produces: `startCmd` reads `<dir>/wm.json`, writes `<state-dir>/<app>.vmargs`
  (`0o600`) with `-setcookie`, boots `erl -detached -boot ... -config ...
  -args_file <release vm.args> -args_file <run vmargs>`, writes the State-File.

- [ ] **Step 1: Write the failing test**

Replace the existing `wm start` unit test(s) in `cli_test.go` (they pass `.go`
sources) with a release-dir test. Add:

```go
func TestStartBootsReleaseDir(t *testing.T) {
	// Build a minimal release dir by hand (no systools needed for the unit test).
	dir := t.TempDir()
	relDir := filepath.Join(dir, "releases", "0.2.5")
	os.MkdirAll(relDir, 0o755)
	os.WriteFile(filepath.Join(relDir, "echo.boot"), []byte("BOOT"), 0o644)
	os.WriteFile(filepath.Join(relDir, "sys.config"), []byte("[{echo, []}].\n"), 0o644)
	os.WriteFile(filepath.Join(relDir, "vm.args"), []byte("-name echo@127.0.0.1\n"), 0o644)
	man := `{"app":"echo","vsn":"0.2.5","node":"echo@127.0.0.1"}`
	os.WriteFile(filepath.Join(dir, "wm.json"), []byte(man), 0o644)

	// Redirect the state dir into the test tmp.
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))

	var gotArgs []string
	orig := runErl
	runErl = func(_ context.Context, _, name string, a ...string) error {
		if strings.HasSuffix(name, "erl") {
			gotArgs = a
		}
		return nil
	}
	t.Cleanup(func() { runErl = orig })

	if err := startCmd(context.Background(), []string{dir}, &bytes.Buffer{}); err != nil {
		t.Fatalf("startCmd: %v", err)
	}

	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-detached", "-boot", "-config", "-args_file"} {
		if !strings.Contains(joined, want) {
			t.Errorf("boot argv missing %q: %v", want, gotArgs)
		}
	}
	if !strings.Contains(joined, filepath.Join(relDir, "echo")) {
		t.Errorf("boot argv missing -boot path: %v", gotArgs)
	}

	// Cookie run-file exists, is 0o600, carries -setcookie, never on argv.
	sd := filepath.Join(os.Getenv("XDG_STATE_HOME"), "wintermute")
	rf := filepath.Join(sd, "echo.vmargs")
	fi, err := os.Stat(rf)
	if err != nil {
		t.Fatalf("cookie run-file missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("run-file mode = %o, want 600", fi.Mode().Perm())
	}
	body, _ := os.ReadFile(rf)
	if !strings.Contains(string(body), "-setcookie") {
		t.Errorf("run-file missing -setcookie: %s", body)
	}
	if strings.Contains(joined, "-setcookie") {
		t.Errorf("cookie must NOT appear on argv: %v", gotArgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestStartBootsReleaseDir -v`
Expected: FAIL — old `startCmd` expects sources, not a dir.

- [ ] **Step 3: Write minimal implementation**

Replace the body of `startCmd` in `cli.go` with:

```go
func startCmd(ctx context.Context, args []string, stdout io.Writer) error {
	version, rest, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: wm start <release-dir> [--version X]")
	}
	dir := rest[0]
	data, err := os.ReadFile(filepath.Join(dir, "wm.json"))
	if err != nil {
		return fmt.Errorf("not a release dir (%s): missing wm.json; run wm release first", dir)
	}
	m, err := release.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("corrupt wm.json in %s: %w", dir, err)
	}
	if !validNodeName(m.Node) {
		return fmt.Errorf("invalid node name %q in wm.json", m.Node)
	}
	relDir := filepath.Join(dir, "releases", m.Vsn)
	boot := filepath.Join(relDir, m.App)
	sysConfig := filepath.Join(relDir, "sys.config")
	relVmArgs := filepath.Join(relDir, "vm.args")

	cookie, err := newCookie()
	if err != nil {
		return err
	}
	sd, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sd, 0o700); err != nil {
		return err
	}
	runVmArgs := filepath.Join(sd, m.App+".vmargs")
	if err := os.WriteFile(runVmArgs, []byte("-setcookie "+cookie+"\n"), 0o600); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	if err := runErl(ctx, ".", l.Erl(), "-detached",
		"-boot", boot, "-config", sysConfig,
		"-args_file", relVmArgs, "-args_file", runVmArgs); err != nil {
		return err
	}
	if err := writeState(m.App, NodeState{Node: m.Node, Cookie: cookie, CodePath: dir}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "started %s (%s)\n", m.App, m.Node)
	return nil
}
```

Add `"go.muehmer.eu/wintermute/internal/pkg/release"` to `cli.go` imports if not
already present. Remove now-unused start-only helpers ONLY if nothing else refers
to them (`buildApp` is still used by `wm build`/`wm release` — keep it).

Update any other existing test in `cli_test.go` that called `wm start` with
sources: delete or convert it to the release-dir shape (the `start_integration_test.go`
rewrite is Task 10).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS (all cli unit tests, including the converted ones).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm start boots a finished release dir, cookie off argv"
```

---

### Task 8: `--version` / usage / README

**Files:**
- Modify: `internal/pkg/cli/cli.go` (`usage`, `commands` list)
- Modify: `README.md`

**Interfaces:** none (docs + help text).

- [ ] **Step 1: Write the failing test**

If `cli_test.go` asserts on usage/commands output, extend it; otherwise add:

```go
func TestUsageListsRelease(t *testing.T) {
	var buf bytes.Buffer
	_ = Run(context.Background(), []string{}, strings.NewReader(""), &buf, &buf)
	if !strings.Contains(buf.String(), "release") {
		t.Errorf("usage should list the release command:\n%s", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestUsageListsRelease -v`
Expected: FAIL — `release` not in usage.

- [ ] **Step 3: Write minimal implementation**

Add `release` to the `commands` list/usage text in `cli.go`. Update `README.md`
so the persistent-node example becomes the two-step flow:

```markdown
wm release echo_app.go echo_sup.go echo_server.go --out build/echo
wm start build/echo
wm status echo
wm call --app echo echo hello
wm stop echo
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestUsageListsRelease -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go README.md
git commit -s -m "docs(cli): document wm release / wm start <dir> two-step flow"
```

---

### Task 9: Ladder rungs VI — release interchangeability (integration)

**Files:**
- Create: `internal/pkg/ladder/ladder_release_integration_test.go`

**Interfaces:**
- Consumes: `runErl`/real OTP at `~/.local/erlang/29.0.3`; reuses the persistent
  fixtures under `testdata/persistent/` (same global echo app + control client).
- Produces: `runRelease(t, idx int, wintermute bool) string` helper and
  `TestRungVI1_ErlangRelease`, `TestRungVI2_WintermuteRelease`, plus a tarball check.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/ladder/ladder_release_integration_test.go` (`//go:build integration`).
Model it on `ladder_persistent_integration_test.go` (`runPersistent`,
`transpilePersistentApp`, the bounded `net_adm:ping` + `global:sync` +
`global:whereis_name` poll). The helper must:

1. Build a release for the echo app:
   - **VI.1 (Erlang):** hand-build a `.rel` from the hand-written
     `testdata/persistent/erlang/*.erl` (compile to a `lib/echo-<vsn>/ebin`,
     write `.rel`/`sys.config`/`vm.args`, run `systools:make_script`).
   - **VI.2 (Wintermute):** invoke the real `cli.releaseCmd`-equivalent flow via
     `erlang.NewLayout` + `release` builders on the Go sources under
     `testdata/persistent/go/`.
2. Boot detached with `erl -boot ... -config ... -args_file <vm.args>
   -args_file <cookie-file>` (unique node name per `idx`+PID, matching the
   persistent ladder's scheme).
3. From a control node, bounded-poll then `gen_server:call({global, echo}, hello)`.
4. Assert the reply is `hello`; stop the node (`rpc:call(Node, init, stop, [])`).

```go
//go:build integration

package ladder

import "testing"

func TestRungVI1_ErlangRelease(t *testing.T) {
	if got := runRelease(t, 1, false); got != "hello" {
		t.Fatalf("VI.1 erlang release = %q, want hello", got)
	}
}

func TestRungVI2_WintermuteRelease(t *testing.T) {
	if got := runRelease(t, 2, true); got != "hello" {
		t.Fatalf("VI.2 wintermute release = %q, want hello", got)
	}
}
```

> Implementer note: reuse the persistent ladder's cookie/poll/node-naming
> helpers verbatim where possible — copy the smallest working subset, do not
> refactor the persistent test. The cookie goes in a `0o600` file passed via
> `-args_file`, never on argv, matching `wm start`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungVI -v`
Expected: FAIL — `runRelease` undefined.

- [ ] **Step 3: Write minimal implementation**

Implement `runRelease` per the note above (real systools + real boot). Iterate
against real OTP until both rungs go green; if `make_script` cannot resolve
kernel/stdlib, add their ebin dirs to `{path,[...]}` (Task 5 note) and mirror the
same fix in `release.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungVI -v`
Expected: PASS (VI.1, VI.2).

- [ ] **Step 5: Add the tarball check + commit**

Add a `TestRungVI_Tarball` that runs `wm release --tar` equivalent, unpacks
`echo-<vsn>.tar.gz` into a fresh dir (stdlib `archive/tar`+`compress/gzip`), and
boots the unpacked release with `erl -boot`, asserting the same `hello`.

```bash
go test -tags integration ./internal/pkg/ladder/ -run TestRungVI -v   # all green
git add internal/pkg/ladder/ladder_release_integration_test.go
git commit -s -m "test(ladder): rungs VI — release interchangeability + tarball"
```

---

### Task 10: CLI e2e — two-step `wm release → wm start` (integration)

**Files:**
- Modify: `internal/pkg/cli/start_integration_test.go`

**Interfaces:**
- Consumes: the real `Run(ctx, args, ...)` entry point on real OTP.
- Produces: `TestReleaseStartCallStopEndToEnd` driving
  `release → start <dir> → status → call → stop`.

- [ ] **Step 1: Write the failing test**

Rewrite `start_integration_test.go`'s end-to-end test to the two-step flow:

```go
//go:build integration

// ... existing setup ...
out := filepath.Join(t.TempDir(), "echo")
srcs := persistentGoSources(t) // the echo app .go files under testdata/persistent/go

// Build the release.
if err := Run(ctx, append([]string{"release", "--out", out, "--vsn", "0.2.5"}, srcs...),
	strings.NewReader(""), &sink, io.Discard); err != nil {
	t.Fatalf("wm release: %v", err)
}
// Boot the finished release.
if err := Run(ctx, []string{"start", out}, strings.NewReader(""), &startOut, io.Discard); err != nil {
	t.Fatalf("wm start: %v", err)
}
defer Run(ctx, []string{"stop", "echo"}, strings.NewReader(""), &sink, io.Discard)
// status → which_applications reachable; call → hello; stop → clean.
```

Keep the existing bounded-retry `status` poll and the `call --app echo echo hello`
→ `hello` assertion; only the boot step changes from `start <sources>` to
`release` + `start <dir>`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags integration ./internal/pkg/cli/ -run EndToEnd -v`
Expected: FAIL — until the two-step flow is wired (references new commands).

- [ ] **Step 3: Make it pass**

Adjust helper(s) (`persistentGoSources`) to point at `testdata/persistent/go`
sources; ensure the app name resolves to `echo`. Run against real OTP.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags integration ./internal/pkg/cli/ -run EndToEnd -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/start_integration_test.go
git commit -s -m "test(cli): e2e wm release -> start <dir> -> call -> stop"
```

---

### Task 11: Verification gate, SDK index, handover

**Files:**
- Modify: `HANDOVER.md`, `docs/SDK-INDEX.md` (regenerate), backlog note.

**Interfaces:** none.

- [ ] **Step 1: Full unit + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: all packages green (`cli`, `erlang`, `transpile`, `release`, `pkg/otp`).

- [ ] **Step 2: Real integration ladder + e2e**

Run: `go test -tags integration ./internal/pkg/ladder/` (all prior rungs + VI) and
`go test -tags integration ./internal/pkg/cli/`.
Expected: PASS. (Requires `~/.local/erlang/29.0.3`; run `./bin/wm erlang install` if absent.)

- [ ] **Step 3: Security scans**

Run: `govulncheck ./...`; `gosec ./...`; `gitleaks detect`.
Expected: no new vulnerability class. Triage any new `G204`/`G304` in `release.go`
as the accepted dual-use CLI class (document in HANDOVER, as 0.2.4 did).

- [ ] **Step 4: Regenerate SDK index**

Run: `/sdk-index` (new `internal/pkg/release` package must appear).

- [ ] **Step 5: Update HANDOVER + backlog, commit**

Update `HANDOVER.md`: 0.2.5 delivered, verification evidence, and **mark the
cookie-on-argv backlog item RESOLVED** (cookie now in a `0o600` `-args_file`
run-file). Add the deferred items (ERTS bundling → 0.2.6, marker-driven
sys.config env, make_script kernel/stdlib path robustness if not needed).

```bash
git add HANDOVER.md docs/SDK-INDEX.md
git commit -s -m "docs: 0.2.5 verification gate + handover (cookie-on-argv closed)"
```

---

## Self-Review

**Spec coverage:**
- Two-step UX (release builds, start boots dir) → Tasks 4–7. ✓
- Metadata release (.rel/boot/sys.config/vm.args/lib layout) → Tasks 2, 4, 5. ✓
- Tarball without ERTS → Task 6, verified Task 9. ✓
- Cookie off argv + out of release → Task 7 (run-file 0o600), asserted; closed in Task 11. ✓
- sys.config empty scaffold, loaded → Task 2 (`SysConfig`), booted with `-config` Task 7/9. ✓
- `wm.json` manifest single-source → Task 3, produced Task 4, consumed Task 7. ✓
- Version discovery (erts/kernel/stdlib) → Task 1, used Task 4. ✓
- No transpiler change → confirmed: no task touches `internal/pkg/transpile`. ✓
- Ladder rungs VI (2 rungs + tarball) → Task 9. ✓
- CLI e2e updated to two-step → Task 10. ✓
- Verification gate → Task 11. ✓

**Placeholder scan:** the two "Implementer note" blocks (Task 4 fixture source,
Task 9 `runRelease`) point at concrete existing files to copy, not invented code —
acceptable, since the transpiler-accepted echo source and the real systools
options must be taken from working fixtures, not fabricated.

**Type consistency:** `Manifest{App,Vsn,Node}` (Task 3) ↔ read in Task 7; `AppVsn
{Name,Vsn}` (Task 2) ↔ used in Task 4; `ErtsVersion`/`AppVersion` (Task 1) ↔
called in Task 4; `releaseCmd`/`startCmd` signatures consistent across Tasks 4–8.
Consistent.
