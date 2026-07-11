# Wintermute 0.2.4 — Persistent Node Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `wm` starts a real, named Erlang node that keeps the transpiled OTP application alive in the background (`erl -detached`), and drives it over Distributed Erlang via `wm start/stop/status/call/attach`.

**Architecture:** Additive `otp` markers (`StartServerGlobal`/`CallGlobal`) let the echo `gen_server` register `{global, echo}`. Five new CLI subcommands, all routed through testable `Runner` seams that assert the assembled `erl`/`erlc` command lines without executing anything. A State-File under `~/.local/state/wintermute/<app>.json` carries node identity (name + cookie + code path) between subcommands. Ladder rungs V.1–V.4 prove cross-node interchangeability on real OTP 29.

**Tech Stack:** Go stdlib only (`go/ast`, `crypto/rand`, `encoding/json`, `os/exec`, `path/filepath`). Erlang/OTP 29 for integration.

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute` (not the GitHub path).
- **Stdlib only.** No third-party Go modules. Tools via `go install` are exempt.
- **TDD, red → green.** Write the failing test, watch it fail, then implement.
- **main() → run() pattern.** All logic in `internal/pkg/`; `main()` only calls `run()`.
- **Build output to `bin/`.** Never bare `go build`.
- **Erlang correctness over Go idiom.** No hidden automatisms; positioned errors, never silent-wrong.
- **Replies to the user in German; code/comments/commits in English.**
- **Default Erlang version:** `erlang.DefaultVersion` (29.0.3). Local build at `~/.local/erlang/29.0.3`.

---

## File Structure

- `pkg/otp/otp.go` — add `StartServerGlobal`, `CallGlobal` markers (+ tests in `otp_test.go`).
- `internal/pkg/transpile/transpile.go` — emit the two new markers (+ tests in `transpile_test.go`).
- `internal/pkg/cli/node.go` **(new)** — State-File type + read/write/remove; capturing and interactive `erl` seams.
- `internal/pkg/cli/node_test.go` **(new)** — unit tests for the State-File and command assembly.
- `internal/pkg/cli/cli.go` — register `start`/`stop`/`status`/`call`/`attach` in the dispatcher; extract a `buildApp` helper reused by `build` and `start`.
- `internal/pkg/cli/cli_test.go` — dispatcher + `buildApp` refactor coverage.
- `testdata/persistent/go/{echoapp,echosup,echoserver,echoclient}/main.go` **(new)** — globally-registered echo fixtures.
- `testdata/persistent/erlang/{echoapp,echosup,echoserver,echoclient}.erl` + `echoapp.app` **(new)** — hand-written counterparts.
- `internal/pkg/ladder/ladder_persistent_integration_test.go` **(new)** — rungs V.1–V.4.
- `README.md`, `HANDOVER.md` — doc refresh at the end.

---

### Task 1: `otp` markers `StartServerGlobal` + `CallGlobal`

**Files:**
- Modify: `pkg/otp/otp.go:29-30`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: `func StartServerGlobal(name string, init any)`, `func CallGlobal(name string, req any) any` — transpile-only markers that panic natively.

- [ ] **Step 1: Write the failing test**

Add to `pkg/otp/otp_test.go` (follow the existing panic-assertion pattern already in that file):

```go
func TestStartServerGlobalPanicsNatively(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StartServerGlobal did not panic")
		}
	}()
	StartServerGlobal("echo", struct{}{})
}

func TestCallGlobalPanicsNatively(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("CallGlobal did not panic")
		}
	}()
	CallGlobal("echo", "hi")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/ -run 'Global' -v`
Expected: FAIL — `undefined: StartServerGlobal` / `undefined: CallGlobal`.

- [ ] **Step 3: Write minimal implementation**

In `pkg/otp/otp.go`, directly after the existing `StartServer`/`Call` lines (otp.go:29-30):

```go
func StartServerGlobal(name string, init any) { transpileOnly("StartServerGlobal") } // -> gen_server:start_link({global,name}, ?MODULE, [], [])
func CallGlobal(name string, req any) any      { transpileOnly("CallGlobal"); return nil } // -> gen_server:call({global,name}, Req)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/otp/ -run 'Global' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/otp.go pkg/otp/otp_test.go
git commit -s -m "feat(otp): add StartServerGlobal and CallGlobal markers"
```

---

### Task 2: Transpile `StartServerGlobal` → `{global, name}`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go:483-490`
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `otp.StartServerGlobal("echo", State{})` in Go source.
- Produces: emitted Erlang `gen_server:start_link({global, echo}, ?MODULE, [], [])`. Unlike `StartServer`, the name is **not** appended to `Result.Registered` (the `.app` `registered` key is for local `register/3` names, which a `{global, …}` server does not use).

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestTranspileStartServerGlobal(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServerGlobal("echo", State{}) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Erl, "gen_server:start_link({global, echo}, ?MODULE, [], [])") {
		t.Fatalf("missing global start_link:\n%s", r.Erl)
	}
	if len(r.Registered) != 0 {
		t.Fatalf("global server must not populate Registered, got %v", r.Registered)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run StartServerGlobal -v`
Expected: FAIL — `unsupported otp call: StartServerGlobal`.

- [ ] **Step 3: Write minimal implementation**

In `transpile.go`, immediately after the `StartServer` block (ends at transpile.go:490), add:

```go
	if sel.Sel.Name == "StartServerGlobal" {
		name, err := em.emitExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("gen_server:start_link({global, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run StartServerGlobal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): emit gen_server:start_link({global, ...}) for StartServerGlobal"
```

---

### Task 3: Transpile `CallGlobal` → `gen_server:call({global, name}, Req)`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go:494-497` (arity map) and `:509-528` (switch)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `otp.CallGlobal("echo", Msg)`.
- Produces: emitted Erlang `gen_server:call({global, echo}, Msg)`. Arity 2; a wrong-arity call yields a positioned error, not a panic.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestTranspileCallGlobal(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.Print(otp.CallGlobal("echo", "hello").(string)) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Erl, "gen_server:call({global, echo}, \"hello\")") {
		t.Fatalf("missing global call:\n%s", r.Erl)
	}
}

func TestTranspileCallGlobalWrongArity(t *testing.T) {
	src := `package c
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.CallGlobal("echo") }
`
	if _, err := Module(src); err == nil {
		t.Fatal("expected positioned arity error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run CallGlobal -v`
Expected: FAIL — `unsupported otp call: CallGlobal` (first test); second test may already pass via the default arity path, but keep it — it locks the contract.

- [ ] **Step 3: Write minimal implementation**

In `transpile.go`, add `"CallGlobal": 2,` to the arity map (transpile.go:494-497):

```go
	arity := map[string]int{
		"Send": 2, "Register": 2, "Whereis": 1, "RegisterGlobal": 2,
		"WhereisGlobal": 1, "Call": 2, "CallGlobal": 2, "Self": 0, "Print": 1,
	}
```

Then add a `case` in the switch, right after the existing `"Call"` case (transpile.go:520-521):

```go
	case "CallGlobal":
		return fmt.Sprintf("gen_server:call({global, %s}, %s)", unquoteAtom(args[0]), args[1]), nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run CallGlobal -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): emit gen_server:call({global, ...}) for CallGlobal"
```

---

### Task 4: State-File type + read/write/remove

**Files:**
- Create: `internal/pkg/cli/node.go`
- Create: `internal/pkg/cli/node_test.go`

**Interfaces:**
- Produces:
  - `type NodeState struct { Node string \`json:"node"\`; Cookie string \`json:"cookie"\`; CodePath string \`json:"codepath"\` }`
  - `func stateDir() (string, error)` — `$XDG_STATE_HOME/wintermute` or `~/.local/state/wintermute`.
  - `func statePath(app string) (string, error)` — `<stateDir>/<app>.json`.
  - `func writeState(app string, s NodeState) error`
  - `func readState(app string) (NodeState, error)` — error names the app and suggests `wm start` when absent.
  - `func removeState(app string) error` — no error if already gone.
  - `func newCookie() (string, error)` — 16 random bytes hex-encoded.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/cli/node_test.go`:

```go
package cli

import (
	"os"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	want := NodeState{Node: "echoapp@127.0.0.1", Cookie: "deadbeef", CodePath: "/tmp/work"}
	if err := writeState("echoapp", want); err != nil {
		t.Fatal(err)
	}
	got, err := readState("echoapp")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if err := removeState("echoapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := readState("echoapp"); err == nil {
		t.Fatal("expected error after remove")
	} else if !strings.Contains(err.Error(), "wm start") {
		t.Fatalf("error should suggest wm start, got %v", err)
	}
}

func TestRemoveStateIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := removeState("nope"); err != nil {
		t.Fatalf("removing absent state should be nil, got %v", err)
	}
}

func TestNewCookieUnique(t *testing.T) {
	a, _ := newCookie()
	b, _ := newCookie()
	if a == "" || a == b {
		t.Fatalf("cookies must be non-empty and unique: %q %q", a, b)
	}
}

var _ = os.Getenv // keep os imported if unused above
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run 'State|Cookie' -v`
Expected: FAIL — `undefined: NodeState` etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/pkg/cli/node.go`:

```go
package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NodeState is the persisted identity of a running wm node, keyed by app name.
type NodeState struct {
	Node     string `json:"node"`
	Cookie   string `json:"cookie"`
	CodePath string `json:"codepath"`
}

// stateDir is $XDG_STATE_HOME/wintermute, defaulting to ~/.local/state/wintermute.
func stateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "wintermute"), nil
}

func statePath(app string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app+".json"), nil
}

func writeState(app string, s NodeState) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p, _ := statePath(app)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func readState(app string) (NodeState, error) {
	p, err := statePath(app)
	if err != nil {
		return NodeState{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return NodeState{}, fmt.Errorf("no running node for %q; run wm start", app)
	}
	var s NodeState
	if err := json.Unmarshal(data, &s); err != nil {
		return NodeState{}, fmt.Errorf("corrupt state for %q: %w", app, err)
	}
	return s, nil
}

func removeState(app string) error {
	p, err := statePath(app)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// newCookie returns 16 cryptographically-random bytes, hex-encoded.
func newCookie() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

Remove the `var _ = os.Getenv` line and the `os` import from the test if `go vet` flags them unused (they are used by `t.Setenv` indirectly — keep only what compiles).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run 'State|Cookie' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/node.go internal/pkg/cli/node_test.go
git commit -s -m "feat(cli): add node State-File (read/write/remove) and cookie generation"
```

---

### Task 5: Capturing + interactive `erl` seams

**Files:**
- Modify: `internal/pkg/cli/node.go`
- Test: `internal/pkg/cli/node_test.go`

**Interfaces:**
- Produces:
  - `var captureErl = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) { ... }` — runs a command and returns combined output. Overridable in tests.
  - `var attachErl = func(ctx context.Context, dir, name string, args ...string) error { ... }` — runs a command wired to the real TTY (`os.Stdin/Stdout/Stderr`). Overridable in tests.

These sit beside the existing `runErl Runner` in `cli.go` (streaming, for `start`/`stop`). `captureErl` serves `status`/`call`; `attachErl` serves `attach`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/cli/node_test.go`:

```go
func TestCaptureErlDefaultRunsCommand(t *testing.T) {
	out, err := captureErl(context.Background(), ".", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("captureErl = %q", out)
	}
}
```

Add `"context"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run CaptureErl -v`
Expected: FAIL — `undefined: captureErl`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/pkg/cli/node.go` (add `"context"` and `"os/exec"` imports):

```go
// captureErl runs a command and returns its combined output. Overridable in
// tests to assert assembled command lines without executing erl.
var captureErl = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	return c.CombinedOutput()
}

// attachErl runs an interactive command wired to the real terminal (used by
// `wm attach` for erl -remsh). Overridable in tests.
var attachErl = func(ctx context.Context, dir, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run CaptureErl -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/node.go internal/pkg/cli/node_test.go
git commit -s -m "feat(cli): add capturing and interactive erl seams"
```

---

### Task 6: Extract `buildApp` helper (DRY with `build`)

**Files:**
- Modify: `internal/pkg/cli/cli.go:66-124` (buildCmd)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Produces: `func buildApp(paths []string, out string) (appMod string, modules, registered []string, err error)` — transpiles each `.go` path to `<out>/<module>.erl` (refusing overwrite), returns the application module name (empty if none), all module names, and all registered names. `buildCmd` is refactored to call it, then emit the `.app` as before.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/cli/cli_test.go`:

```go
func TestBuildAppReturnsAppModule(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte(`package echoapp
import "go.muehmer.eu/wintermute/pkg/otp"
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(App{}) }
func (App) Stop() {}
`), 0o644)
	out := t.TempDir()
	appMod, modules, _, err := buildApp([]string{src}, out)
	if err != nil {
		t.Fatal(err)
	}
	if appMod != "echoapp" {
		t.Fatalf("appMod = %q, want echoapp", appMod)
	}
	if len(modules) != 1 || modules[0] != "echoapp" {
		t.Fatalf("modules = %v", modules)
	}
	if _, err := os.Stat(filepath.Join(out, "echoapp.erl")); err != nil {
		t.Fatalf("echoapp.erl not written: %v", err)
	}
}
```

> Note: if `App` above does not satisfy the transpiler's application-detection (needs `Start`/`Stop` methods per the 0.2.3 spec), copy the exact shape from `testdata/otpapp/go/echoapp/main.go` instead. Read that file first and mirror it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run BuildApp -v`
Expected: FAIL — `undefined: buildApp`.

- [ ] **Step 3: Write minimal implementation**

In `cli.go`, extract the transpile loop from `buildCmd` into:

```go
// buildApp transpiles each Go path to <out>/<module>.erl (refusing to
// overwrite) and reports the application module (empty if none), all module
// names, and all registered names. Shared by `wm build` and `wm start`.
func buildApp(paths []string, out string) (appMod string, modules, registered []string, err error) {
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return "", nil, nil, err
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			return "", nil, nil, err
		}
		dst := outPath(out, r.Module)
		if _, err := os.Stat(dst); err == nil {
			return "", nil, nil, fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
		}
		if err := os.WriteFile(dst, []byte(r.Erl), 0o644); err != nil {
			return "", nil, nil, err
		}
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			if appMod != "" {
				return "", nil, nil, fmt.Errorf("more than one application module (%s and %s)", appMod, r.Module)
			}
			appMod = r.Module
		}
	}
	return appMod, modules, registered, nil
}
```

Then rewrite `buildCmd`'s body (keeping its flag parsing, `MkdirAll`, and `.app` emission) to call `buildApp` and print each `.erl` path. The `.erl` paths printed to stdout must be preserved — capture them by re-deriving with `outPath(out, mod)` for each module, or have `buildApp` return them. Simplest: after `buildApp`, loop `for _, m := range modules { fmt.Fprintln(stdout, outPath(out, m)) }`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS — the new `TestBuildAppReturnsAppModule` **and** all existing build tests (they guard the refactor).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "refactor(cli): extract buildApp helper shared by build and start"
```

---

### Task 7: `wm start` — boot detached node + write State-File

**Files:**
- Modify: `internal/pkg/cli/cli.go` (dispatcher `commands` map, `usage`, `switch`; new `startCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `buildApp` (Task 6), `NodeState`/`writeState`/`newCookie` (Task 4), `runErl` (existing streaming seam).
- Produces: `func startCmd(ctx context.Context, args []string, stdout io.Writer) error`. Assembles `erlc` (one per module) then `erl -detached -name <node> -setcookie <c> -pa <out> -eval "application:start(<appMod>)"`, then `writeState`. Node name default `<appMod>@127.0.0.1`, overridable via `--name`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/cli/cli_test.go` (mirror `TestRunAssemblesErlcAndErl`, cli_test.go:61):

```go
func TestStartAssemblesDetachedErlAndWritesState(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	// Mirror the app fixture shape from testdata/otpapp/go/echoapp/main.go.
	src := filepath.Join(dir, "main.go")
	appSrc, err := os.ReadFile("../../../testdata/otpapp/go/echoapp/main.go")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, appSrc, 0o644)

	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"start", "--out", t.TempDir(), src},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	last := cmds[len(cmds)-1]
	for _, want := range []string{"-detached", "-name echoapp@127.0.0.1", "-setcookie ", "application:start(echoapp)"} {
		if !strings.Contains(last, want) {
			t.Fatalf("erl cmd missing %q:\n%s", want, last)
		}
	}
	st, err := readState("echoapp")
	if err != nil {
		t.Fatalf("state not written: %v", err)
	}
	if st.Node != "echoapp@127.0.0.1" || st.Cookie == "" {
		t.Fatalf("bad state: %+v", st)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run StartAssembles -v`
Expected: FAIL — dispatcher returns `unknown command: "start"`.

- [ ] **Step 3: Write minimal implementation**

In `cli.go`: add `"start": "start a persistent node hosting an OTP application"` to the `commands` map; add `"start"` to the `usage` slice; add `case "start": return startCmd(ctx, args[1:], stdout)` to the switch. Then:

```go
// startCmd transpiles + compiles the given Go sources inline, boots a detached,
// named Erlang node running application:start(<app>), and records the node in a
// State-File so stop/status/call/attach can find it with no args.
func startCmd(ctx context.Context, args []string, stdout io.Writer) error {
	name, rest, err := parseStringFlag(args, "--name", "")
	if err != nil {
		return err
	}
	version, rest, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wm start <path>... [--name N] [--out DIR] [--version X]")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	appMod, modules, _, err := buildApp(rest, out)
	if err != nil {
		return err
	}
	if appMod == "" {
		return fmt.Errorf("no application module among %v; wm start needs one", rest)
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	for _, m := range modules {
		if err := runErl(ctx, ".", l.Erlc(), "-o", out, outPath(out, m)); err != nil {
			return err
		}
	}
	if name == "" {
		name = appMod + "@127.0.0.1"
	}
	cookie, err := newCookie()
	if err != nil {
		return err
	}
	eval := fmt.Sprintf("application:start(%s)", appMod)
	if err := runErl(ctx, ".", l.Erl(), "-detached", "-name", name,
		"-setcookie", cookie, "-pa", out, "-eval", eval); err != nil {
		return err
	}
	if err := writeState(appMod, NodeState{Node: name, Cookie: cookie, CodePath: out}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "started %s (%s)\n", appMod, name)
	return nil
}
```

Add a shared `parseStringFlag(args []string, flag, def string) (string, []string, error)` helper (this also resolves the deferred DRY item from the 0.2.3 backlog — but keep `parseVsnFlag`/`parseOutFlag` as-is for this task to avoid churn; only add the new generic helper here):

```go
// parseStringFlag pulls an optional "--flag V" / "--flag=V" out of args.
func parseStringFlag(args []string, flag, def string) (string, []string, error) {
	val := def
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == flag:
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a value", flag)
			}
			val = args[i+1]
			i++
		case strings.HasPrefix(a, flag+"="):
			val = strings.TrimPrefix(a, flag+"=")
		default:
			rest = append(rest, a)
		}
	}
	return val, rest, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run StartAssembles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm start — boot detached node and record State-File"
```

---

### Task 8: `wm stop` — rpc init:stop + remove State-File

**Files:**
- Modify: `internal/pkg/cli/cli.go` (dispatcher; new `stopCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `readState`/`removeState` (Task 4), `runErl`.
- Produces: `func stopCmd(ctx context.Context, args []string, stdout io.Writer) error`. Reads the State-File (app name = first positional, or the sole running app), assembles a short-lived control node `erl -name <ctrl> -setcookie <c> -noshell -eval 'rpc:call(<Node>, init, stop, []), init:stop().'`, then removes the State-File.

- [ ] **Step 1: Write the failing test**

```go
func TestStopAssemblesRpcAndRemovesState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"stop", "echoapp"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmds, "\n")
	for _, want := range []string{"-setcookie c0ffee", "rpc:call('echoapp@127.0.0.1', init, stop, [])"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stop cmd missing %q:\n%s", want, joined)
		}
	}
	if _, err := readState("echoapp"); err == nil {
		t.Fatal("state should be removed after stop")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run StopAssembles -v`
Expected: FAIL — `unknown command: "stop"`.

- [ ] **Step 3: Write minimal implementation**

Register `stop` in the dispatcher (map + usage + `case "stop": return stopCmd(ctx, args[1:], stdout)`). Add a helper to resolve the target app, then `stopCmd`:

```go
// resolveApp returns the app name to act on: the first positional arg, or the
// sole running app if none is given.
func resolveApp(args []string) (string, []string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:], nil
	}
	dir, err := stateDir()
	if err != nil {
		return "", nil, err
	}
	entries, _ := os.ReadDir(dir)
	var apps []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			apps = append(apps, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	switch len(apps) {
	case 0:
		return "", nil, fmt.Errorf("no running node; run wm start")
	case 1:
		return apps[0], args, nil
	default:
		return "", nil, fmt.Errorf("multiple running nodes %v; name one explicitly", apps)
	}
}

// ctrlNode returns a unique-ish control node name for a short-lived erl.
func ctrlNode() string { return "wmctrl@127.0.0.1" }

func stopCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	eval := fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", st.Node)
	if err := runErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval); err != nil {
		return err
	}
	if err := removeState(app); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "stopped %s\n", app)
	return nil
}
```

> `ctrlNode()` is a fixed name for now; the integration tests (Task 12) will need unique control-node names to avoid `epmd` collisions — parameterize there or accept sequential test execution. Note this in the deferrals.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run StopAssembles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm stop — rpc init:stop and clear State-File"
```

---

### Task 9: `wm status` — ping + which_applications

**Files:**
- Modify: `internal/pkg/cli/cli.go` (dispatcher; new `statusCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `readState`, `captureErl` (Task 5), `resolveApp` (Task 8).
- Produces: `func statusCmd(ctx context.Context, args []string, stdout io.Writer) error`. Assembles a control node that prints ping + running apps: `erl -name <ctrl> -setcookie <c> -noshell -eval '<report>, init:stop().'`, and writes the captured output to stdout. On `pang` it points at the log file.

- [ ] **Step 1: Write the failing test**

```go
func TestStatusAssemblesPingAndReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	var gotArgs []string
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		gotArgs = a
		return []byte("pong\n"), nil
	}
	defer func() {
		captureErl = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			c := execCombined(ctx, dir, name, args...)
			return c, nil
		}
	}()

	var out strings.Builder
	if err := Run(context.Background(), []string{"status", "echoapp"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "net_adm:ping('echoapp@127.0.0.1')") {
		t.Fatalf("status cmd missing ping:\n%s", joined)
	}
	if !strings.Contains(out.String(), "pong") {
		t.Fatalf("status out = %q", out.String())
	}
}
```

> Simpler alternative for the `defer` restore: capture the original into a variable before overriding — `orig := captureErl; defer func() { captureErl = orig }()`. Use that; drop the `execCombined` reference above (it does not exist).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run StatusAssembles -v`
Expected: FAIL — `unknown command: "status"`.

- [ ] **Step 3: Write minimal implementation**

Register `status` in the dispatcher. Then:

```go
func statusCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	eval := fmt.Sprintf(
		"io:format(\"~p~n\", [net_adm:ping('%s')]), "+
			"io:format(\"~p~n\", [rpc:call('%s', application, which_applications, [])]), "+
			"init:stop().", st.Node, st.Node)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval)
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	fmt.Fprintf(stdout, "%s (%s):\n%s", app, st.Node, out)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run StatusAssembles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm status — ping node and list running applications"
```

---

### Task 10: `wm call` — cross-node gen_server:call

**Files:**
- Modify: `internal/pkg/cli/cli.go` (dispatcher; new `callCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `readState`, `captureErl`, `resolveApp`.
- Produces: `func callCmd(ctx context.Context, args []string, stdout io.Writer) error`. Usage: `wm call <name> <request>` (`<name>` = globally-registered gen_server name; `<request>` = a string sent as a binary). Assembles a control node running `gen_server:call({global, <name>}, <<"...">>)` and prints the reply. The app to connect through is resolved from the sole running State-File (or `--app`).

- [ ] **Step 1: Write the failing test**

```go
func TestCallAssemblesGlobalGenServerCall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	orig := captureErl
	var gotArgs []string
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		gotArgs = a
		return []byte("hi\n"), nil
	}
	defer func() { captureErl = orig }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"call", "echo", "hi"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, `gen_server:call({global, echo}, <<"hi">>)`) {
		t.Fatalf("call cmd missing global call:\n%s", joined)
	}
	if strings.TrimSpace(out.String()) != "hi" {
		t.Fatalf("call out = %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run CallAssembles -v`
Expected: FAIL — `unknown command: "call"`.

- [ ] **Step 3: Write minimal implementation**

Register `call` in the dispatcher. Then:

```go
func callCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := parseStringFlag(args, "--app", "")
	if err != nil {
		return err
	}
	version, rest, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: wm call <name> <request> [--app APP]")
	}
	name, req := rest[0], rest[1]
	if app == "" {
		app, _, err = resolveApp(nil)
		if err != nil {
			return err
		}
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	eval := fmt.Sprintf(
		"io:format(\"~s~n\", [gen_server:call({global, %s}, <<%q>>)]), init:stop().",
		name, req)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval)
	if err != nil {
		return fmt.Errorf("call failed: %w", err)
	}
	fmt.Fprint(stdout, string(out))
	return nil
}
```

> The `<<%q>>` produces `<<"hi">>`. Verify the exact quoting against the test assertion; if `%q` double-escapes, build the binary literal explicitly as `"<<\"" + req + "\">>"`. The test pins the required output.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run CallAssembles -v`
Expected: PASS. Adjust the eval string until the assertion matches.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm call — cross-node gen_server:call({global, ...})"
```

---

### Task 11: `wm attach` — interactive remote shell

**Files:**
- Modify: `internal/pkg/cli/cli.go` (dispatcher; new `attachCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Consumes: `readState`, `attachErl` (Task 5), `resolveApp`.
- Produces: `func attachCmd(ctx context.Context, args []string, stdout io.Writer) error`. Assembles `erl -remsh <Node> -name <ctrl> -setcookie <c>` wired to the real TTY.

- [ ] **Step 1: Write the failing test**

```go
func TestAttachAssemblesRemsh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	orig := attachErl
	var gotArgs []string
	attachErl = func(_ context.Context, _, _ string, a ...string) error {
		gotArgs = a
		return nil
	}
	defer func() { attachErl = orig }()

	if err := Run(context.Background(), []string{"attach", "echoapp"},
		strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-remsh echoapp@127.0.0.1", "-setcookie c0ffee"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("attach cmd missing %q:\n%s", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run AttachAssembles -v`
Expected: FAIL — `unknown command: "attach"`.

- [ ] **Step 3: Write minimal implementation**

Register `attach` in the dispatcher. Then:

```go
func attachCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	// A unique-per-invocation control node avoids clashing with a prior attach.
	ctrl := "wmattach@127.0.0.1"
	return attachErl(ctx, ".", l.Erl(), "-remsh", st.Node, "-name", ctrl, "-setcookie", st.Cookie)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run AttachAssembles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): wm attach — interactive erl -remsh to the running node"
```

---

### Task 12: Persistent-node fixtures (global echo)

**Files:**
- Create: `testdata/persistent/go/echoserver/main.go`, `.../echoapp/main.go`, `.../echosup/main.go`, `.../echoclient/main.go`
- Create: `testdata/persistent/erlang/echoserver.erl`, `echosup.erl`, `echoapp.erl`, `echoclient.erl`, `echoapp.app`

**Interfaces:**
- Produces: a globally-registered echo app. The only difference from `testdata/otpapp/` is `{global, echo}` registration and a `CallGlobal` client. Read every file under `testdata/otpapp/` first and mirror it, changing only the registration.

- [ ] **Step 1: Read the 0.2.3 fixtures**

Read all of `testdata/otpapp/go/*/main.go` and `testdata/otpapp/erlang/*` so the new fixtures match structure exactly.

- [ ] **Step 2: Create the Go fixtures**

`testdata/persistent/go/echoserver/main.go` — identical to the otpapp echoserver except:

```go
func Start() { otp.StartServerGlobal("echo", State{}) }
```

`testdata/persistent/go/echoclient/main.go` — the client calls globally:

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.CallGlobal("echo", "hello").(string)) }
```

`echoapp/main.go` and `echosup/main.go` — copy verbatim from `testdata/otpapp/go/{echoapp,echosup}/main.go` (their child spec still points at `echoserver`).

- [ ] **Step 3: Create the Erlang counterparts**

Mirror `testdata/otpapp/erlang/*`, changing only:

`testdata/persistent/erlang/echoserver.erl`:

```erlang
-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

init(_) -> {ok, {state, 0}}.
handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.

start() -> gen_server:start_link({global, echo}, ?MODULE, [], []).
```

`testdata/persistent/erlang/echoclient.erl`:

```erlang
-module(echoclient).
-export([main/0]).

main() -> io:format("~s~n", [gen_server:call({global, echo}, <<"hello">>)]).
```

Copy `echoapp.erl`, `echosup.erl`, `echoapp.app` verbatim from `testdata/otpapp/erlang/` (module names unchanged).

- [ ] **Step 4: Verify fixtures transpile**

Build the binary and transpile each Go fixture to confirm they compile through the transpiler:

Run:
```bash
go build -o bin/wm ./cmd/wm
./bin/wm build testdata/persistent/go/echoserver/main.go testdata/persistent/go/echoclient/main.go --out $(mktemp -d)
```
Expected: prints two `.erl` paths, no error; the echoserver output contains `{global, echo}`.

- [ ] **Step 5: Commit**

```bash
git add testdata/persistent
git commit -s -m "test(fixtures): globally-registered echo app for persistent-node ladder"
```

---

### Task 13: Ladder rungs V.1–V.4 (integration)

**Files:**
- Create: `internal/pkg/ladder/ladder_persistent_integration_test.go`

**Interfaces:**
- Consumes: the persistent fixtures (Task 12), `erlang.NewLayout`, `transpile.Module`/`AppResource`. Mirrors the helpers in `ladder_otpapp_integration_test.go` but boots a **detached** node with a **unique** name, runs a cross-node call from a control node, asserts the reply, and tears the node down.

- [ ] **Step 1: Write the four failing rung tests + helper**

Create `internal/pkg/ladder/ladder_persistent_integration_test.go` with build tag `//go:build integration`. The helper `runPersistent(t, serverErls, appFile, callerErls, callerNode)`:

1. `t.Skip` if `!l.Installed()`.
2. `erlc` all server `.erl` into a temp workdir; place `echoapp.app`.
3. Pick a unique node name: `fmt.Sprintf("echoapp_%d@127.0.0.1", os.Getpid()+idx)` — pass an index per rung so parallel rungs never collide.
4. Boot detached: `exec.Command(l.Erl(), "-detached", "-name", node, "-setcookie", cookie, "-pa", work, "-eval", "application:start(echoapp)")`.
5. `t.Cleanup`: `exec.Command(l.Erl(), "-name", ctrl, "-setcookie", cookie, "-noshell", "-eval", fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", node)).Run()`.
6. Poll `net_adm:ping` from a control node until `pong` (up to ~2s) before calling — the detached node needs a beat to register globally.
7. Run the caller: for an Erlang caller, `erlc` + `echoclient:main()` on a control node connected to `node`; for the reply, capture stdout. Assert it equals `hello`.

Write the four rungs:

```go
func TestRungV1_ErlangToErlang(t *testing.T)   { assertPersistent(t, 1, erlPersistServer(), erlPersistApp, erlPersistClient) }
func TestRungV2_WintermuteCaller(t *testing.T) { /* transpile client, erl server */ }
func TestRungV3_WintermuteServer(t *testing.T) { /* transpile server+app, erl client */ }
func TestRungV4_BothWintermute(t *testing.T)   { /* transpile both */ }
```

Model the transpile helpers on `transpileApp`/`transpileToErl` in `ladder_otpapp_integration_test.go` (read that file; reuse its `transpileToErl` if exported within the package — it is same-package, so call it directly).

> Because this is integration-only and needs live global registration timing, write the helper carefully: the ping-poll loop (step 6) is what makes the cross-node call reliable. Without it the call races the global name registration and flakes.

- [ ] **Step 2: Run to verify they fail (or skip)**

Run: `go test -tags integration ./internal/pkg/ladder/ -run RungV -v`
Expected: FAIL (compile error / missing helper) if OTP present; `SKIP` if not installed. If skipped, you cannot proceed — ensure `~/.local/erlang/29.0.3` exists (`./bin/wm erlang install`).

- [ ] **Step 3: Implement the helper until rungs pass**

Fill in the helper and per-rung transpile wiring. Iterate on node-name uniqueness and the ping-poll until all four pass.

- [ ] **Step 4: Run to verify they pass**

Run: `go test -tags integration ./internal/pkg/ladder/ -run RungV -v`
Expected: PASS ×4. Then run the full integration suite to confirm IV.1–IV.4 still pass:
`go test -tags integration ./internal/pkg/ladder/`
Expected: all 20 rungs PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/ladder/ladder_persistent_integration_test.go
git commit -s -m "test(ladder): rungs V.1-V.4 — cross-node persistent-node interchangeability"
```

---

### Task 14: Docs + final verification gate

**Files:**
- Modify: `README.md` (usage: add `wm start/stop/status/call/attach`)
- Modify: `HANDOVER.md` (0.2.4 delivered; next step)

- [ ] **Step 1: Update README usage**

Add to the `## Usage` block in `README.md`:

```bash
# Start a persistent node hosting an OTP application
wm start echo_app.go echo_sup.go echo_server.go

wm status              # is it up? which apps run?
wm call echo "hi"      # cross-node gen_server:call({global, echo}, ...)
wm attach              # interactive remote shell (detach leaves it running)
wm stop                # clean shutdown
```

- [ ] **Step 2: Run the full verification gate**

Run and confirm each is green:
```bash
go build -o bin/wm ./cmd/wm
go test ./...
go test -tags integration ./internal/pkg/ladder/
govulncheck ./...
gitleaks detect
gosec ./...
```
Expected: unit + integration green; `govulncheck`/`gitleaks` clean; `gosec` findings unchanged in class from 0.2.3 (path/perms in file-writing paths — the State-File write is the same accepted class).

- [ ] **Step 3: Update HANDOVER.md**

Record: 0.2.4 delivered (persistent node, detached-first, five subcommands, State-File, global markers, rungs V.1–V.4 = 20 rungs total green on OTP 29). Next step: 0.2.5 = C (full OTP release), conditional on this holding. Move any new deferrals into the backlog (fixed `ctrlNode`/`wmattach` names → unique; `wm ls`; `-heart`; log rotation).

- [ ] **Step 4: Commit**

```bash
git add README.md HANDOVER.md
git commit -s -m "docs: 0.2.4 usage + handover — persistent node shipped"
```

---

## Self-Review

**Spec coverage:**
- CLI surface (start/stop/status/call/attach) → Tasks 7–11. ✓
- State-File identity → Task 4. ✓
- Marker API (`StartServerGlobal`/`CallGlobal`) → Tasks 1–3. ✓
- Separate fixtures → Task 12. ✓
- Ladder V.1–V.4 → Task 13. ✓
- Detached diagnosis / error handling → `wm status` reports reachability (`net_adm:ping` → `pong`/`pang`) and names the node (Task 9); `stop`/`status`/`call`/`attach` on a missing State-File return actionable errors (Task 4). **Scope decision:** the detached-node **log file** is downgraded to a backlog item (Task 14 Step 3) — the OTP 29 kernel-logger flag string is fragile and not worth pinning for the echo subset; `wm status` reporting `pang` with the node name is the diagnosis surface for 0.2.4. Revisit when a real app needs startup-failure logs.
- Testing seam (`runErl`/`captureErl`/`attachErl`) → Task 5. ✓
- Deferrals → Task 14 Step 3. ✓

**Placeholder scan:** No `TBD`/`TODO`. Task 13 intentionally sketches per-rung wiring (integration timing is environment-dependent) but pins the exact commands, the ping-poll requirement, and the pass criterion (20 rungs green). Tasks 8–11 pin exact assertion strings so the eval quoting is unambiguous.

**Type consistency:** `NodeState{Node,Cookie,CodePath}` used identically across Tasks 4/7/8/9/10/11. `buildApp` signature (Task 6) matches its call in Task 7. `captureErl`/`attachErl`/`runErl` seams consistent. `resolveApp`/`ctrlNode` defined in Task 8, reused in 9/11. `parseStringFlag` defined in Task 7, reused in 10.
