# Wintermute 0.1.0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a single-node Go→Erlang echo-interop ladder: build a local Erlang from source, and transpile valid-Go echo programs to Erlang that interoperate with hand-written Erlang, up to Wintermute↔Wintermute.

**Architecture:** Two decoupled subsystems wired by the `wm` CLI. `internal/pkg/transpile` turns a valid-Go source file (using `pkg/otp`) into an Erlang module string via `go/parser`+`go/ast`. `internal/pkg/erlang` builds/locates a local Erlang under `~/.local/erlang/<ver>/` and runs `erlc`/`erl`. The echo ladder (rungs 1–4) is the end-to-end proof.

**Tech Stack:** Go stdlib only (`go/parser`, `go/ast`, `go/token`, `os/exec`, `net/http`, `archive/tar`, `compress/gzip`, `testing`). Local Erlang/OTP as the runtime target.

## Global Constraints

- **Stdlib only.** No third-party Go modules. Tools installed via `go install` are exempt (not project deps).
- **main() → run() pattern.** `main()` only calls `run()` + `os.Exit`; all logic in `internal/pkg/`, dependencies (`context.Context`, `args`, `io.Reader`/`io.Writer`, command runner) injected.
- **TDD, red → green first.** Write the failing test, watch it fail, then implement.
- **DRY & KISS + Google Go Style Guide.**
- **Binaries + generated artifacts → `bin/`** (gitignored). No temporary files in the project root.
- **Project language: technically correct English** (code, comments, commits, docs).
- **Git:** develop on `origin` (`git@git.nebula.muehmer.eu:bsmr/wintermute.git`), branch `development-0.1.0-work`. `upstream`/`github` are gated release targets — never pushed from tasks. Copilot review gate applies only before github-bound commits, not per-task origin commits.
- **Module path:** `go.muehmer.eu/wintermute` (not the github repo path).
- **Verified sources:** any external URL/version goes through `docs/verified-sources.md` before use. OTP source pattern (verified 2026-07-10): `https://github.com/erlang/otp/releases/download/OTP-<ver>/otp_src_<ver>.tar.gz`; default version `29.0.3`.

## Ponytail simplifications (deliberate, with ceilings)

- `// ponytail: 0.1.0 transpiler matches the echo subset via go/ast; go/types-based resolution is later hardening for arbitrary programs.`
- `// ponytail: single-clause receive only; multi-clause/pattern receive is 0.2.0.`
- The real OTP build (Task 6) and ladder E2E (Tasks 7, 16) are gated behind the `integration` build tag — slow + network + local Erlang required.

## File structure

```text
pkg/otp/otp.go                         # marker API (Pid, Self, Spawn, Register, Whereis, Send, Receive, Print)
pkg/otp/otp_test.go
internal/pkg/erlang/source.go          # version + tarball URL resolution
internal/pkg/erlang/paths.go           # ~/.local/erlang/<ver>/ layout
internal/pkg/erlang/build.go           # build orchestration via injected Runner
internal/pkg/erlang/toolchain.go       # locate erlc/erl for a version
internal/pkg/erlang/*_test.go
internal/pkg/erlang/build_integration_test.go   //go:build integration
internal/pkg/transpile/transpile.go    # File(src) (string, error): Go AST -> Erlang module
internal/pkg/transpile/transpile_test.go
internal/pkg/cli/cli.go                # dispatch: erlang install/list, build, run (extend existing)
internal/pkg/cli/cli_test.go
testdata/echo/erlang/echoserver.erl    # rung 1 hand-written Erlang
testdata/echo/erlang/echoclient.erl
testdata/echo/go/echoserver/main.go    # Wintermute (valid Go) echo server
testdata/echo/go/echoclient/main.go    # Wintermute echo client
internal/pkg/ladder/ladder_integration_test.go  //go:build integration  # rungs 1–4
bin/                                   # generated .erl/.beam (gitignored)
```

---

## Task 1: `otp` marker package

**Files:**
- Create: `pkg/otp/otp.go`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: `otp.Pid` (struct), `otp.Self() Pid`, `otp.Spawn(fn func()) Pid`, `otp.Register(name string, p Pid)`, `otp.Whereis(name string) Pid`, `otp.Send(to Pid, msg any)`, `otp.Receive() any`, `otp.Print(s string)`. Bodies are transpile-only stubs (panic if run natively).

- [ ] **Step 1: Write the failing test**

```go
package otp

import "testing"

func TestMarkersPanicNatively(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Self() to panic natively (transpile-only marker)")
		}
	}()
	_ = Self()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/`
Expected: FAIL — `undefined: Self`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package otp is the Erlang/OTP-in-Go marker API. Programs written with it are
// valid Go for tooling, but execute only after transpilation to Erlang; the
// bodies here panic if run natively.
package otp

const transpileOnly = "otp: transpile-only marker; run via the Wintermute transpiler"

// Pid is an opaque Erlang process identifier.
type Pid struct{ _ struct{} }

func Self() Pid                    { panic(transpileOnly) } // -> self()
func Spawn(fn func()) Pid          { panic(transpileOnly) } // -> spawn(fun ... end)
func Register(name string, p Pid)  { panic(transpileOnly) } // -> register(name, Pid)
func Whereis(name string) Pid      { panic(transpileOnly) } // -> whereis(name)
func Send(to Pid, msg any)         { panic(transpileOnly) } // -> To ! Msg
func Receive() any                 { panic(transpileOnly) } // -> receive <clause> end
func Print(s string)               { panic(transpileOnly) } // -> io:format("~s~n", [S])
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/otp/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/
git commit -s -m "feat(otp): add transpile-only OTP marker package"
```

---

## Task 2: Erlang source URL + version resolution

**Files:**
- Create: `internal/pkg/erlang/source.go`
- Test: `internal/pkg/erlang/source_test.go`

**Interfaces:**
- Produces: `erlang.DefaultVersion = "29.0.3"`; `erlang.SourceURL(version string) string`.

- [ ] **Step 1: Write the failing test**

```go
package erlang

import "testing"

func TestSourceURL(t *testing.T) {
	got := SourceURL("29.0.3")
	want := "https://github.com/erlang/otp/releases/download/OTP-29.0.3/otp_src_29.0.3.tar.gz"
	if got != want {
		t.Fatalf("SourceURL = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/`
Expected: FAIL — `undefined: SourceURL`.

- [ ] **Step 3: Write minimal implementation**

```go
package erlang

import "fmt"

// DefaultVersion is the OTP release verified in docs/verified-sources.md.
const DefaultVersion = "29.0.3"

// SourceURL returns the official OTP source tarball URL for a version.
func SourceURL(version string) string {
	return fmt.Sprintf(
		"https://github.com/erlang/otp/releases/download/OTP-%s/otp_src_%s.tar.gz",
		version, version)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/erlang/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): resolve OTP source tarball URL by version"
```

---

## Task 3: Local install layout paths

**Files:**
- Create: `internal/pkg/erlang/paths.go`
- Test: `internal/pkg/erlang/paths_test.go`

**Interfaces:**
- Produces: `type Layout struct { Root, Src, Bin string }`; `func NewLayout(home, version string) Layout`.

- [ ] **Step 1: Write the failing test**

```go
package erlang

import (
	"path/filepath"
	"testing"
)

func TestNewLayout(t *testing.T) {
	l := NewLayout("/home/u", "29.0.3")
	if l.Root != filepath.FromSlash("/home/u/.local/erlang/29.0.3") {
		t.Fatalf("Root = %q", l.Root)
	}
	if l.Src != filepath.Join(l.Root, "src") {
		t.Fatalf("Src = %q", l.Src)
	}
	if l.Bin != filepath.Join(l.Root, "bin") {
		t.Fatalf("Bin = %q", l.Bin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestNewLayout`
Expected: FAIL — `undefined: NewLayout`.

- [ ] **Step 3: Write minimal implementation**

```go
package erlang

import "path/filepath"

// Layout is the on-disk structure for one installed Erlang version.
type Layout struct {
	Root string // ~/.local/erlang/<version>
	Src  string // retained sources for debugging
	Bin  string // installed erl/erlc (configure --prefix target)
}

// NewLayout builds the layout for a version under home.
func NewLayout(home, version string) Layout {
	root := filepath.Join(home, ".local", "erlang", version)
	return Layout{Root: root, Src: filepath.Join(root, "src"), Bin: filepath.Join(root, "bin")}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/erlang/ -run TestNewLayout`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): define ~/.local/erlang/<ver> layout"
```

---

## Task 4: Build step command assembly (injected runner)

**Files:**
- Create: `internal/pkg/erlang/build.go`
- Test: `internal/pkg/erlang/build_test.go`

**Interfaces:**
- Consumes: `Layout`, `SourceURL`.
- Produces: `type Runner func(ctx context.Context, dir, name string, args ...string) error`; `type Builder struct { Home string; Out io.Writer; Run Runner }`; `func (b Builder) Build(ctx context.Context, version string) error`. `Build` records the commands via `Run`: `configure` (with `--prefix`), `make`, `make install`, run inside `Layout.Src`.

- [ ] **Step 1: Write the failing test**

```go
package erlang

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestBuildAssemblesConfigureMakeInstall(t *testing.T) {
	var cmds []string
	b := Builder{
		Home: "/home/u",
		Out:  io.Discard,
		Run: func(_ context.Context, dir, name string, args ...string) error {
			cmds = append(cmds, dir+"|"+name+" "+strings.Join(args, " "))
			return nil
		},
	}
	if err := b.Build(context.Background(), "29.0.3"); err != nil {
		t.Fatal(err)
	}
	src := "/home/u/.local/erlang/29.0.3/src"
	want := []string{
		src + "|./configure --prefix=/home/u/.local/erlang/29.0.3",
		src + "|make",
		src + "|make install",
	}
	if len(cmds) != len(want) {
		t.Fatalf("commands = %v", cmds)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Fatalf("cmd[%d] = %q, want %q", i, cmds[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestBuild`
Expected: FAIL — `undefined: Builder`.

- [ ] **Step 3: Write minimal implementation**

```go
package erlang

import (
	"context"
	"io"
)

// Runner executes an external command in dir. Injected for testability.
type Runner func(ctx context.Context, dir, name string, args ...string) error

// Builder builds a local Erlang from source into ~/.local/erlang/<ver>.
type Builder struct {
	Home string
	Out  io.Writer
	Run  Runner
}

// Build runs configure/make/make install in the extracted source tree.
// Download + extraction is done by Task 6's real Runner before this;
// Build only assembles and drives the compile/install steps.
func (b Builder) Build(ctx context.Context, version string) error {
	l := NewLayout(b.Home, version)
	if err := b.Run(ctx, l.Src, "./configure", "--prefix="+l.Root); err != nil {
		return err
	}
	if err := b.Run(ctx, l.Src, "make"); err != nil {
		return err
	}
	return b.Run(ctx, l.Src, "make", "install")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/erlang/ -run TestBuild`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): assemble configure/make/install build steps"
```

---

## Task 5: Toolchain lookup

**Files:**
- Create: `internal/pkg/erlang/toolchain.go`
- Test: `internal/pkg/erlang/toolchain_test.go`

**Interfaces:**
- Consumes: `Layout`.
- Produces: `func (l Layout) Erlc() string`; `func (l Layout) Erl() string`; `func (l Layout) Installed() bool` (true if `Erl()` exists and is executable).

- [ ] **Step 1: Write the failing test**

```go
package erlang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalled(t *testing.T) {
	home := t.TempDir()
	l := NewLayout(home, "29.0.3")
	if l.Installed() {
		t.Fatal("should not be installed on empty dir")
	}
	if err := os.MkdirAll(l.Bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Erl(), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !l.Installed() {
		t.Fatal("should be installed after writing erl")
	}
	if l.Erlc() != filepath.Join(l.Bin, "erlc") {
		t.Fatalf("Erlc = %q", l.Erlc())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestInstalled`
Expected: FAIL — `l.Installed undefined`.

- [ ] **Step 3: Write minimal implementation**

```go
package erlang

import (
	"os"
	"path/filepath"
)

func (l Layout) Erlc() string { return filepath.Join(l.Bin, "erlc") }
func (l Layout) Erl() string  { return filepath.Join(l.Bin, "erl") }

// Installed reports whether erl exists in this layout's bin.
func (l Layout) Installed() bool {
	info, err := os.Stat(l.Erl())
	return err == nil && !info.IsDir()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/erlang/ -run TestInstalled`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): locate erlc/erl and detect installed versions"
```

---

## Task 6: Real OTP build (gated integration)

**Files:**
- Create: `internal/pkg/erlang/build_integration_test.go`
- Modify: `internal/pkg/erlang/build.go` (add `Download` + `Extract` real runner helper `Provision`)

**Interfaces:**
- Produces: `func (b Builder) Provision(ctx context.Context, version string) error` — download tarball via `net/http`, extract via `archive/tar`+`compress/gzip` into `Layout.Src`, then `Build`. Verifies URL from `SourceURL`.

- [ ] **Step 1: Write the failing (gated) test**

```go
//go:build integration

package erlang

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestProvisionRealBuild(t *testing.T) {
	home := t.TempDir()
	b := Builder{
		Home: home,
		Out:  os.Stderr,
		Run: func(ctx context.Context, dir, name string, args ...string) error {
			c := exec.CommandContext(ctx, name, args...)
			c.Dir, c.Stdout, c.Stderr = dir, os.Stderr, os.Stderr
			return c.Run()
		},
	}
	if err := b.Provision(context.Background(), DefaultVersion); err != nil {
		t.Fatal(err)
	}
	if !NewLayout(home, DefaultVersion).Installed() {
		t.Fatal("erl not installed after Provision")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags integration ./internal/pkg/erlang/ -run TestProvisionRealBuild`
Expected: FAIL — `b.Provision undefined`.

- [ ] **Step 3: Implement `Provision`**

```go
// (append to build.go)
import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Provision downloads and extracts the OTP source, then builds+installs it.
func (b Builder) Provision(ctx context.Context, version string) error {
	l := NewLayout(b.Home, version)
	if err := os.MkdirAll(l.Src, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SourceURL(version), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", SourceURL(version), resp.StatusCode)
	}
	if err := extractTarGz(resp.Body, l.Src, "otp_src_"+version+"/"); err != nil {
		return err
	}
	return b.Build(ctx, version)
}

// extractTarGz unpacks a .tar.gz, stripping the leading prefix into dst.
func extractTarGz(r io.Reader, dst, strip string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}
		name := strings.TrimPrefix(h.Name, strip)
		if name == "" || name == h.Name {
			continue
		}
		target := filepath.Join(dst, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}
```

- [ ] **Step 4: Run to verify it passes** (slow, ~10–30 min, needs build deps: autoconf, gcc, make, libncurses, libssl)

Run: `go test -tags integration -timeout 60m ./internal/pkg/erlang/ -run TestProvisionRealBuild`
Expected: PASS; `~/.local/erlang/29.0.3/bin/erl` exists.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): download+extract+build OTP from source (gated)"
```

---

## Task 7: Rung 1 — hand-written Erlang echo (gated E2E)

**Files:**
- Create: `testdata/echo/erlang/echoserver.erl`, `testdata/echo/erlang/echoclient.erl`
- Create: `internal/pkg/ladder/ladder_integration_test.go`

**Interfaces:**
- Consumes: `erlang.NewLayout`, `erlang.Layout.Erlc/Erl`.
- Produces: a reusable gated helper `runEcho(t, serverErl, clientErl string) string` that compiles both `.erl` with local `erlc`, boots `erl`, and returns stdout.

- [ ] **Step 1: Write the failing (gated) test + fixtures**

`testdata/echo/erlang/echoserver.erl`:

```erlang
-module(echoserver).
-export([serve/0, start/0]).

serve() ->
    receive
        {echo, From, Text} -> From ! {ok, Text}, serve()
    end.

start() -> register(echo, spawn(fun echoserver:serve/0)).
```

`testdata/echo/erlang/echoclient.erl`:

```erlang
-module(echoclient).
-export([main/0]).

main() ->
    whereis(echo) ! {echo, self(), <<"hello">>},
    receive {ok, Text} -> io:format("~s~n", [Text]) end.
```

`internal/pkg/ladder/ladder_integration_test.go`:

```go
//go:build integration

package ladder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
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
```

- [ ] **Step 2: Run to verify it fails/skips**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRung1`
Expected: SKIP if Erlang not built yet; after Task 6 build, FAIL first if fixtures wrong, then iterate to PASS.

- [ ] **Step 3: Make it pass**

Ensure fixtures compile and boot as above (`echoserver:start/0` registers, `echoclient:main/0` echoes). No Go code to add — fixtures are the implementation.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRung1`
Expected: PASS — output `hello`.

- [ ] **Step 5: Commit**

```bash
git add testdata/echo/erlang/ internal/pkg/ladder/
git commit -s -m "test(ladder): rung 1 Erlang<->Erlang echo E2E (gated)"
```

---

## Task 8: Transpiler — module + exported function skeleton

**Files:**
- Create: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `func File(src string) (string, error)` — parses one Go file, emits an Erlang module: `-module(<pkg>).`, `-export([...]).` for exported (capitalized) funcs, and one clause per func (body added in later tasks). Function names are lowercased; arity 0 for 0.1.0.

- [ ] **Step 1: Write the failing test**

```go
package transpile

import "testing"

func TestFile_ModuleAndExport(t *testing.T) {
	src := `package echoserver
func Serve() {}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoserver).\n-export([serve/0]).\n\nserve() ->\n    ok.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_Module`
Expected: FAIL — `undefined: File`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package transpile turns a valid-Go source file (using pkg/otp) into an
// Erlang module. 0.1.0 targets the echo subset via go/ast.
// ponytail: go/ast pattern-matching for the echo subset; go/types resolution
// is later hardening for arbitrary programs.
package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// File parses Go source and emits an Erlang module string.
func File(src string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		return "", err
	}
	var exports []string
	var bodies strings.Builder
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := strings.ToLower(fn.Name.Name)
		if fn.Name.IsExported() {
			exports = append(exports, name+"/0")
		}
		stmts, err := emitBody(fn.Body)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&bodies, "\n%s() ->\n%s.\n", name, indent(stmts))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "-module(%s).\n", f.Name.Name)
	fmt.Fprintf(&b, "-export([%s]).\n", strings.Join(exports, ", "))
	b.WriteString(bodies.String())
	return b.String(), nil
}

// emitBody returns the Erlang expression sequence for a function body.
// Extended in later tasks; empty body -> "ok".
func emitBody(body *ast.BlockStmt) (string, error) {
	if body == nil || len(body.List) == 0 {
		return "ok", nil
	}
	return "ok", nil
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestFile_Module`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): emit Erlang module + exports + empty clauses"
```

---

## Task 9: Transpiler — otp calls + tail self-call loop

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitBody`, add `emitStmt`, `emitExpr`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File`.
- Produces: statement/expression emission for: `otp.Send(a, b)` → `A ! B`; `otp.Register(name, p)` → `register(name, P)`; `otp.Whereis(name)` → `whereis(name)`; `otp.Self()` → `self()`; `otp.Spawn(F)` → `spawn(fun ?MODULE:f/0)`; `otp.Print(x)` → `io:format("~s~n", [X])`; a bare self-call `Serve()` → `serve()`. String literals → `<<"...">>`. Statements in a body are comma-joined; the last has no trailing comma (handled by `File`'s ` .`).

- [ ] **Step 1: Write the failing test**

```go
func TestFile_ClientBody(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() {
	otp.Send(otp.Whereis("echo"), "hello")
	otp.Print("done")
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoclient).\n-export([main/0]).\n\n" +
		"main() ->\n" +
		"    whereis(echo) ! <<\"hello\">>,\n" +
		"    io:format(\"~s~n\", [<<\"done\">>]).\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_ClientBody`
Expected: FAIL — body emits `ok` not the calls.

- [ ] **Step 3: Extend the emitter**

```go
// Replace emitBody and add emitStmt/emitExpr.
func emitBody(body *ast.BlockStmt) (string, error) {
	if body == nil || len(body.List) == 0 {
		return "ok", nil
	}
	var parts []string
	for _, s := range body.List {
		e, err := emitStmt(s)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ",\n"), nil
}

func emitStmt(s ast.Stmt) (string, error) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		return emitExpr(st.X)
	default:
		return "", fmt.Errorf("unsupported statement: %T", s)
	}
}

func emitExpr(e ast.Expr) (string, error) {
	switch ex := e.(type) {
	case *ast.BasicLit:
		if ex.Kind == token.STRING {
			return "<<" + ex.Value + ">>", nil // ex.Value keeps the quotes
		}
		return "", fmt.Errorf("unsupported literal: %s", ex.Value)
	case *ast.CallExpr:
		return emitCall(ex)
	default:
		return "", fmt.Errorf("unsupported expression: %T", e)
	}
}

func emitCall(c *ast.CallExpr) (string, error) {
	// bare self-call: Serve()
	if id, ok := c.Fun.(*ast.Ident); ok {
		return strings.ToLower(id.Name) + "()", nil
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", fmt.Errorf("unsupported call target: %T", c.Fun)
	}
	pkg, _ := sel.X.(*ast.Ident)
	if pkg == nil || pkg.Name != "otp" {
		return "", fmt.Errorf("unsupported call: %s", sel.Sel.Name)
	}
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		s, err := emitExpr(a)
		if err != nil {
			return "", err
		}
		args[i] = s
	}
	switch sel.Sel.Name {
	case "Send":
		return args[0] + " ! " + args[1], nil
	case "Register":
		return fmt.Sprintf("register(%s, %s)", unquoteAtom(args[0]), args[1]), nil
	case "Whereis":
		return fmt.Sprintf("whereis(%s)", unquoteAtom(args[0])), nil
	case "Self":
		return "self()", nil
	case "Spawn":
		return fmt.Sprintf("spawn(fun ?MODULE:%s/0)", strings.ToLower(argIdent(c.Args[0]))), nil
	case "Print":
		return fmt.Sprintf("io:format(\"~s~n\", [%s])", args[0], ), nil
	default:
		return "", fmt.Errorf("unsupported otp call: %s", sel.Sel.Name)
	}
}

// unquoteAtom turns <<"echo">> back into the bare atom echo (for register/whereis names).
func unquoteAtom(s string) string {
	s = strings.TrimPrefix(s, "<<\"")
	s = strings.TrimSuffix(s, "\">>")
	return s
}

// argIdent returns the identifier name of a function-value argument (Spawn(Serve)).
func argIdent(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
```

Note: `emitBody` joins with `,\n`; `File` appends ` .` after the indented block via its `%s.` format, yielding the trailing `.`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestFile_ClientBody`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): emit otp calls, self-calls, string binaries"
```

---

## Task 10: Transpiler — struct literals → tagged tuples

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr` composite lit + selector fields)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `Echo{From: otp.Self(), Text: "hello"}` → `{echo, self(), <<"hello">>}` (leading atom = lowercased type name; fields in the struct's declared order). Field access `req.From` / `.Text` → the bound Erlang variables from the receive pattern (Task 11).

- [ ] **Step 1: Write the failing test**

```go
func TestFile_StructLiteralToTuple(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "whereis(echo) ! {echo, self(), <<\"hello\">>}") {
		t.Fatalf("missing tuple emission:\n%s", got)
	}
}
```

(Add `import "strings"` to the test file if not present.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_StructLiteral`
Expected: FAIL — composite literal unsupported.

- [ ] **Step 3: Extend `emitExpr`**

`File` must first collect struct field order. Add a package-level pass:

```go
// In File, before emitting bodies, build struct field order:
structs := map[string][]string{} // typeName -> ordered field names
for _, d := range f.Decls {
	gd, ok := d.(*ast.GenDecl)
	if !ok || gd.Tok != token.TYPE {
		continue
	}
	for _, sp := range gd.Specs {
		ts, ok := sp.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		var fields []string
		for _, fld := range st.Fields.List {
			for _, n := range fld.Names {
				fields = append(fields, n.Name)
			}
		}
		structs[ts.Name.Name] = fields
	}
}
```

Thread `structs` into the emitter (make emit functions methods on a small `emitter` struct holding `structs`, or pass it down). Then in `emitExpr`:

```go
case *ast.CompositeLit:
	typ, ok := ex.Type.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("unsupported composite literal")
	}
	order := em.structs[typ.Name]
	byField := map[string]string{}
	for _, elt := range ex.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return "", fmt.Errorf("struct literal needs field: value")
		}
		key := kv.Key.(*ast.Ident).Name
		v, err := em.emitExpr(kv.Value)
		if err != nil {
			return "", err
		}
		byField[key] = v
	}
	parts := []string{strings.ToLower(typ.Name)}
	for _, fn := range order {
		parts = append(parts, byField[fn])
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
case *ast.SelectorExpr:
	// req.From -> the bound variable From (capitalized field name)
	if _, ok := ex.X.(*ast.Ident); ok {
		return ex.Sel.Name, nil // field name is already Erlang-variable-cased (From, Text)
	}
	return "", fmt.Errorf("unsupported selector")
```

Convert the free `emit*` functions into methods on `type emitter struct { structs map[string][]string }`, and construct one `em` in `File`. Update call sites accordingly.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestFile_StructLiteral`
Expected: PASS. Re-run full package: `go test ./internal/pkg/transpile/` — all green.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): struct literals to tagged Erlang tuples"
```

---

## Task 11: Transpiler — single-clause receive

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmt` for `:=` receive, `emitBody`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: the pattern
  ```go
  req := otp.Receive().(Echo)
  otp.Send(req.From, Ok{Text: req.Text})
  ```
  → `receive {echo, From, Text} -> From ! {ok, Text} end`. The `:= otp.Receive().(T)` assignment opens a `receive` clause whose pattern is the tuple for `T` with each field bound to its capitalized field name; the following statements become the clause body; the receive spans to the end of the function body.
  `// ponytail: single-clause receive only; multi-clause/pattern receive is 0.2.0.`

- [ ] **Step 1: Write the failing test**

```go
func TestFile_ServerReceiveLoop(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
type Ok struct { Text string }
func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}
`
	got, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "serve() ->\n" +
		"    receive\n" +
		"        {echo, From, Text} ->\n" +
		"            From ! {ok, Text},\n" +
		"            serve()\n" +
		"    end.\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant contains:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_ServerReceive`
Expected: FAIL — `:=` statement unsupported.

- [ ] **Step 3: Extend `emitBody`/`emitStmt`**

In `emitBody`, detect a leading `receive` assignment and wrap the remaining statements as its clause body:

```go
func (em emitter) emitBody(body *ast.BlockStmt) (string, error) {
	if body == nil || len(body.List) == 0 {
		return "ok", nil
	}
	// Detect: first stmt is `x := otp.Receive().(T)`
	if pat, rest, ok := em.receiveHead(body.List); ok {
		inner, err := em.emitStmts(rest)
		if err != nil {
			return "", err
		}
		clauseBody := strings.ReplaceAll(inner, "\n", "\n            ")
		return "receive\n        " + pat + " ->\n            " + clauseBody + "\n    end", nil
	}
	return em.emitStmts(body.List)
}

func (em emitter) emitStmts(list []ast.Stmt) (string, error) {
	var parts []string
	for _, s := range list {
		e, err := em.emitStmt(s)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ",\n"), nil
}

// receiveHead recognizes `x := otp.Receive().(T)` and returns the Erlang tuple
// pattern for T plus the remaining statements.
func (em emitter) receiveHead(list []ast.Stmt) (pattern string, rest []ast.Stmt, ok bool) {
	as, ok := list[0].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return "", nil, false
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return "", nil, false
	}
	call, ok := ta.X.(*ast.CallExpr)
	if !ok || !isOtpCall(call, "Receive") {
		return "", nil, false
	}
	typ, ok := ta.Type.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	parts := []string{strings.ToLower(typ.Name)}
	for _, fld := range em.structs[typ.Name] {
		parts = append(parts, fld) // field name is the bound Erlang variable
	}
	return "{" + strings.Join(parts, ", ") + "}", list[1:], true
}

func isOtpCall(c *ast.CallExpr, name string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "otp" && sel.Sel.Name == name
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS (all transpiler tests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): single-clause receive loop from otp.Receive"
```

---

## Task 12: Transpiler — golden full echo modules

**Files:**
- Create: `testdata/echo/go/echoserver/main.go`, `testdata/echo/go/echoclient/main.go`
- Modify: `internal/pkg/transpile/transpile_test.go` (golden test)

**Interfaces:**
- Consumes: `File`.
- Produces: the two Wintermute programs used by the ladder, and a golden test that `File(main.go)` emits the exact Erlang from Task 7's fixtures (modulo the `start/0` wiring, which lives in the server program).

- [ ] **Step 1: Write the fixtures + failing golden test**

`testdata/echo/go/echoserver/main.go`:

```go
package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}

func Start() { otp.Register("echo", otp.Spawn(Serve)) }
```

`testdata/echo/go/echoclient/main.go`:

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
	otp.Print(otp.Receive().(Ok).Text)
}
```

Golden test:

```go
func TestFile_GoldenServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/echo/go/echoserver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-module(echoserver).",
		"receive\n        {echo, From, Text} ->",
		"From ! {ok, Text}",
		"start() -> register(echo, spawn(fun ?MODULE:serve/0)).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
```

(Add `import "os"` to the test file.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_Golden`
Expected: FAIL — `otp.Print(otp.Receive().(Ok).Text)` (Receive as expression, not head) or `start/0` single-line form not yet emitted.

- [ ] **Step 3: Handle remaining cases**

Support `otp.Receive().(T).Field` as an expression (non-head receive is not in the client since `Main` uses it inline — emit an inline `receive` is out of 0.1.0 scope; instead the client keeps the receive as the *last* statement). Adjust the client fixture so the reply receive is the body head:

```go
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
	reply := otp.Receive().(Ok)
	otp.Print(reply.Text)
}
```

This reuses Task 11's receive-head (the `otp.Send` precedes it, so generalize `receiveHead` to scan for the receive assignment among leading statements: emit statements before it, then the receive with the rest as its clause body). Update `emitBody`:

```go
func (em emitter) emitBody(body *ast.BlockStmt) (string, error) {
	if body == nil || len(body.List) == 0 {
		return "ok", nil
	}
	for i, s := range body.List {
		if as, ok := s.(*ast.AssignStmt); ok && em.isReceiveAssign(as) {
			pre, err := em.emitStmts(body.List[:i])
			if err != nil {
				return "", err
			}
			pat, _, _ := em.receiveHead(body.List[i:])
			inner, err := em.emitStmts(body.List[i+1:])
			if err != nil {
				return "", err
			}
			clause := strings.ReplaceAll(inner, "\n", "\n            ")
			recv := "receive\n        " + pat + " ->\n            " + clause + "\n    end"
			if pre == "" {
				return recv, nil
			}
			return pre + ",\n" + recv, nil
		}
	}
	return em.emitStmts(body.List)
}

func (em emitter) isReceiveAssign(as *ast.AssignStmt) bool {
	if as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return false
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return false
	}
	c, ok := ta.X.(*ast.CallExpr)
	return ok && isOtpCall(c, "Receive")
}
```

Also emit a zero-body single-statement function (`Start`) on one line if its body is a single expression: keep the standard multi-line form — the golden test matches `start() -> register(...).` so special-case a single-statement body:

```go
// In File, when emitting a clause whose body is a single line, use one-line form:
line := indent(stmts)
if !strings.Contains(stmts, "\n") {
	fmt.Fprintf(&bodies, "\n%s() -> %s.\n", name, stmts)
} else {
	fmt.Fprintf(&bodies, "\n%s() ->\n%s.\n", name, line)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS (all, including golden).

- [ ] **Step 5: Commit**

```bash
git add testdata/echo/go/ internal/pkg/transpile/
git commit -s -m "feat(transpile): golden echo server/client modules"
```

---

## Task 13: CLI — `wm build <path>`

**Files:**
- Modify: `internal/pkg/cli/cli.go`, `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `transpile.File`.
- Produces: `wm build <path>` reads the Go file at `<path>`, writes `bin/<module>.erl`, prints the output path. Errors on missing file / transpile error.

- [ ] **Step 1: Write the failing test**

```go
func TestBuildCommand(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Serve() {}\n"), 0o644)
	var out strings.Builder
	err := Run(context.Background(), []string{"build", src}, strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	erl := filepath.Join("bin", "m.erl")
	b, err := os.ReadFile(erl)
	if err != nil {
		t.Fatalf("expected %s: %v", erl, err)
	}
	if !strings.Contains(string(b), "-module(m).") {
		t.Fatalf("bad erl:\n%s", b)
	}
	os.Remove(erl)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestBuildCommand`
Expected: FAIL — `build` currently returns "not implemented yet".

- [ ] **Step 3: Implement `build` dispatch**

```go
// In Run's dispatch, replace the stub for "build":
case "build":
	return buildCmd(args[1:], stdout)
```

```go
func buildCmd(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: wm build <path>")
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	erl, err := transpile.File(string(src))
	if err != nil {
		return err
	}
	mod := moduleName(erl)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	outPath := filepath.Join("bin", mod+".erl")
	if err := os.WriteFile(outPath, []byte(erl), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, outPath)
	return nil
}

// moduleName extracts the name from "-module(name)." on the first line.
func moduleName(erl string) string {
	first := erl
	if i := strings.IndexByte(erl, '\n'); i >= 0 {
		first = erl[:i]
	}
	first = strings.TrimPrefix(first, "-module(")
	return strings.TrimSuffix(first, ").")
}
```

Add imports `os`, `path/filepath`, `strings`, and the transpile package to `cli.go`. Keep the existing `commands` map (add `"build"` handled specially before the stub switch).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): wm build transpiles a Go file to bin/<module>.erl"
```

---

## Task 14: CLI — `wm erlang install|list`

**Files:**
- Modify: `internal/pkg/cli/cli.go`, `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `erlang.NewLayout`, `erlang.DefaultVersion`, `erlang.Builder.Provision`.
- Produces: `wm erlang list` prints installed versions under `~/.local/erlang/`; `wm erlang install [--version X]` provisions (delegates to `erlang.Builder`). `list` is unit-tested against a temp `HOME`; `install` wiring is tested for arg parsing only (real build is the gated Task 6).

- [ ] **Step 1: Write the failing test**

```go
func TestErlangList(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".local", "erlang", "29.0.3", "bin"), 0o755)
	os.WriteFile(filepath.Join(home, ".local", "erlang", "29.0.3", "bin", "erl"), []byte("x"), 0o755)
	t.Setenv("HOME", home)
	var out strings.Builder
	err := Run(context.Background(), []string{"erlang", "list"}, strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "29.0.3") {
		t.Fatalf("list = %q", out.String())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestErlangList`
Expected: FAIL — `erlang` unknown command.

- [ ] **Step 3: Implement `erlang` subcommand**

```go
// dispatch:
case "erlang":
	return erlangCmd(context.Background(), args[1:], stdout)
```

```go
func erlangCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wm erlang <install|list>")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		root := filepath.Join(home, ".local", "erlang")
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.IsDir() && erlang.NewLayout(home, e.Name()).Installed() {
				fmt.Fprintln(stdout, e.Name())
			}
		}
		return nil
	case "install":
		version := erlang.DefaultVersion
		if len(args) == 3 && args[1] == "--version" {
			version = args[2]
		}
		b := erlang.Builder{Home: home, Out: stdout, Run: execRunner}
		return b.Provision(ctx, version)
	default:
		return fmt.Errorf("unknown erlang subcommand: %q", args[0])
	}
}

// execRunner runs a real command, streaming output.
func execRunner(ctx context.Context, dir, name string, cmdArgs ...string) error {
	c := exec.CommandContext(ctx, name, cmdArgs...)
	c.Dir = dir
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}
```

Add imports `context`, `os/exec`, and the erlang package.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): wm erlang install|list for local Erlang versions"
```

---

## Task 15: CLI — `wm run <path>`

**Files:**
- Modify: `internal/pkg/cli/cli.go`, `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `transpile.File`, `erlang.NewLayout`.
- Produces: `wm run <path> [--version X]` transpiles `<path>` to `bin/<mod>.erl`, compiles with the version's `erlc` into `bin/`, and boots `erl -noshell -pa bin -eval '<mod>:main(), init:stop().'`. Unit test covers transpile+write+command assembly with an injected runner; real boot is gated (Task 16).

- [ ] **Step 1: Write the failing test**

```go
func TestRunAssemblesErlcAndErl(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Main() { }\n"), 0o644)
	t.Setenv("HOME", t.TempDir())
	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()
	var out strings.Builder
	if err := Run(context.Background(), []string{"run", src}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || !strings.Contains(cmds[0], "erlc") || !strings.Contains(cmds[1], "m:main()") {
		t.Fatalf("cmds = %v", cmds)
	}
	os.Remove(filepath.Join("bin", "m.erl"))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestRunAssembles`
Expected: FAIL — `run` is a stub; `runErl` undefined.

- [ ] **Step 3: Implement `run` dispatch**

```go
// package-level indirection for testability:
var runErl Runner = execRunner
type Runner = func(ctx context.Context, dir, name string, args ...string) error

// dispatch:
case "run":
	return runCmd(context.Background(), args[1:], stdout)
```

```go
func runCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: wm run <path> [--version X]")
	}
	version := erlang.DefaultVersion
	if len(args) == 3 && args[1] == "--version" {
		version = args[2]
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	erl, err := transpile.File(string(src))
	if err != nil {
		return err
	}
	mod := moduleName(erl)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("bin", mod+".erl"), []byte(erl), 0o644); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	if err := runErl(ctx, ".", l.Erlc(), "-o", "bin", filepath.Join("bin", mod+".erl")); err != nil {
		return err
	}
	eval := mod + ":main(), init:stop()."
	return runErl(ctx, ".", l.Erl(), "-noshell", "-pa", "bin", "-eval", eval)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/pkg/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): wm run transpiles, compiles, and boots on local Erlang"
```

---

## Task 16: Rungs 2–4 — Wintermute interop (gated E2E)

**Files:**
- Modify: `internal/pkg/ladder/ladder_integration_test.go`

**Interfaces:**
- Consumes: `transpile.File`, the `runEcho` helper (Task 7), the Go fixtures (Task 12).
- Produces: `transpileToErl(t, goPath, dir) string` — transpile a Wintermute program to `<dir>/<mod>.erl`; rungs 2/3/4 assemble server+client `.erl` from mixed Erlang/Wintermute origins and assert output `hello`.

- [ ] **Step 1: Write the failing (gated) tests**

```go
//go:build integration

// (append to ladder_integration_test.go)

func transpileToErl(t *testing.T, goPath, dir string) string {
	t.Helper()
	src, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	erl, err := transpile.File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	mod := moduleNameFromErl(erl)
	out := filepath.Join(dir, mod+".erl")
	if err := os.WriteFile(out, []byte(erl), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func moduleNameFromErl(erl string) string {
	first := strings.SplitN(erl, "\n", 2)[0]
	return strings.TrimSuffix(strings.TrimPrefix(first, "-module("), ").")
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
```

Add imports `go.muehmer.eu/wintermute/internal/pkg/transpile` and `os`.

Note: `runEcho` (Task 7) boots `echoserver:start(), echoclient:main()`. The Wintermute-transpiled server must emit the same `start/0` (it does — from `Start()` in the fixture) and module name `echoserver`; the client emits `echoclient:main/0`. Because both origins produce identical module names, functions, and message tuples, the four rungs are interchangeable.

- [ ] **Step 2: Run to verify it fails/skips**

Run: `go test -tags integration ./internal/pkg/ladder/`
Expected: SKIP without local Erlang; with it, iterate until PASS.

- [ ] **Step 3: Reconcile any emission mismatch**

If a rung fails, diff the transpiled `.erl` against the Task 7 hand-written fixture; adjust the transpiler (not the fixture) so transpiled output is byte-compatible where it matters (module/function/tuple shapes). This closes the loop that transpiled ≡ hand-written.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags integration ./internal/pkg/ladder/`
Expected: PASS — rungs 1–4 all echo `hello`.

- [ ] **Step 5: Commit + tag readiness**

```bash
git add internal/pkg/ladder/
git commit -s -m "test(ladder): rungs 2-4 Wintermute interop E2E (gated)"
git push origin development-0.1.0-work
```

---

## Self-review notes

- **Spec coverage:** provisioning (Tasks 2–6), rung 0/1 (6–7), transpiler subset — module/func/otp/struct/receive/loop (8–12), CLI build/erlang/run (13–15), rungs 2–4 (16). `check`/`new`/`repl` intentionally remain stubs (spec §CLI surface).
- **Gated integration:** real OTP build + all `erl` boots are behind `-tags integration` and skip without a local Erlang — fast unit suite (`go test ./...`) stays green offline.
- **Naming:** modules `echoserver`/`echoclient`; funcs `serve/0`,`start/0`,`main/0`; tuples `{echo,From,Text}`/`{ok,Text}` are consistent across fixtures, transpiler tests, and ladder.
- **Verify before release:** run `govulncheck ./...`, `gosec ./...`, `gitleaks detect`, `semgrep --config auto`, and the Copilot gate before any github-bound promotion (not per task).
