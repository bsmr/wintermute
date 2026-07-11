# Wintermute 0.2.3 — OTP Application deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transpile a Go `application → supervisor → gen_server` triple to Erlang/OTP and prove interchangeability with hand-written Erlang at ladder rungs IV.1–IV.4, including emission of a minimal `.app` resource file.

**Architecture:** Extend the go/ast emitter in `internal/pkg/transpile` with two new behaviour branches (application, supervisor) driven by method-set convention (mirroring the 0.2.2 gen_server branch). Add two `pkg/otp` markers (`StartSupervisor`, `Child`). Generate the `.app` resource from information already in the source (VERSION, `otp.StartServer` names, module list). Extend `wm build` to accept multiple files and emit the `.app`. Add integration rungs IV.1–IV.4 booting via `application:start/1`.

**Tech Stack:** Go stdlib only (`go/ast`, `go/parser`, `go/token`, `strings`, `fmt`); real Erlang/OTP 29.0.3 for integration.

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute` (not the GitHub path).
- **Stdlib only.** No third-party Go modules.
- **TDD, red → green:** write the failing test, run it, watch it fail, then implement.
- **main() → run() pattern:** all logic in `internal/pkg/`; `main()` unchanged.
- **Build output to `bin/`:** `go build -o bin/wm ./cmd/wm`, never bare `go build`.
- **Never silent-wrong:** unsupported input errors with a `file:line:col` position; no hidden identifier transforms (reject, don't auto-fix).
- **Fixed 0.2.3 supervisor values** (documented limitations, not silent guesses): SupFlags `{one_for_one, 1, 5}`; child spec `permanent, 5000, worker, [<childmod>]`.
- **`.app` constants:** `applications` is always `[kernel, stdlib]`; `mod` is `{<appmodule>, []}`.
- Commit messages use conventional commits; sign off with `-s`.

---

### Task 1: New OTP markers `StartSupervisor` and `Child`

**Files:**
- Modify: `pkg/otp/otp.go`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: `func StartSupervisor(sup any) Pid` (transpile-only marker, panics natively); `type Child struct { ID string; Start func() }` (a plain data struct describing a supervisor child; no method bodies).

- [ ] **Step 1: Write the failing test**

Add to `pkg/otp/otp_test.go`:

```go
func TestStartSupervisorPanics(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "wm build") || !strings.Contains(msg, "StartSupervisor") {
			t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
		}
	}()
	_ = StartSupervisor(nil)
}

func TestChildIsPlainData(t *testing.T) {
	c := Child{ID: "echo", Start: func() {}}
	if c.ID != "echo" || c.Start == nil {
		t.Fatal("Child should hold an ID and a Start func without panicking")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/ -run 'TestStartSupervisorPanics|TestChildIsPlainData' -v`
Expected: FAIL — `StartSupervisor` and `Child` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

In `pkg/otp/otp.go`, after the gen_server markers (after the `Call` line):

```go
// Child describes one supervised process for a supervisor's Init. Start is the
// child's start function (e.g. echoserver.Start); it maps to the child spec MFA.
type Child struct {
	ID    string
	Start func()
}

func StartSupervisor(sup any) Pid { transpileOnly("StartSupervisor"); return Pid{} } // -> Sup:start_link()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/otp/ -v`
Expected: PASS (all marker tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/otp.go pkg/otp/otp_test.go
git commit -s -m "feat(otp): add StartSupervisor and Child markers"
```

---

### Task 2: Supervisor behaviour emission

**Files:**
- Modify: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go` (add cases)

**Interfaces:**
- Consumes: existing `emitter`, `methodNamed`, `returnExprs`, `otpPkgIdent`, `unquoteAtom`.
- Produces: internal helpers `isSupervisorInit(fn *ast.FuncDecl) bool` and `(em *emitter) supervisorChildren(fn *ast.FuncDecl) ([]string, error)`; the behaviour dispatch in `File`/`Module` now recognizes a `Sup{ Init() []otp.Child }` type and emits `-behaviour(supervisor)` + `start_link/0` + `init/1`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestSupervisorBehaviour(t *testing.T) {
	src := `package echosup
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echoserver"
type Sup struct{}
func (Sup) Init() []otp.Child {
	return []otp.Child{{ID: "echo", Start: echoserver.Start}}
}
`
	erl, mod, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echosup" {
		t.Fatalf("mod = %q, want echosup", mod)
	}
	for _, want := range []string{
		"-behaviour(supervisor).",
		"start_link() -> supervisor:start_link({local, echosup}, ?MODULE, []).",
		"init(_) -> {ok, {{one_for_one, 1, 5}, [{echo, {echoserver, start, []}, permanent, 5000, worker, [echoserver]}]}}.",
	} {
		if !strings.Contains(erl, want) {
			t.Fatalf("missing %q in:\n%s", want, erl)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestSupervisorBehaviour -v`
Expected: FAIL — currently the methods loop returns `type Sup has methods but no Init; not a recognized gen_server`? No — `Init` exists but returns `[]otp.Child`; the gen_server branch will try `returnExprs`/`emitExpr` on the slice literal and error. Either way: not the expected Erlang.

- [ ] **Step 3: Write minimal implementation**

In `transpile.go`, restructure the `for typeName, ms := range methods` loop to dispatch on behaviour. Replace the current gen_server-only body (the block starting `initFn := methodNamed(ms, "Init")`) with:

```go
for typeName, ms := range methods {
	switch {
	case isSupervisorInit(methodNamed(ms, "Init")):
		behaviour = "-behaviour(supervisor).\n"
		exports = append(exports, "start_link/0", "init/1")
		children, err := em.supervisorChildren(methodNamed(ms, "Init"))
		if err != nil {
			return Result{}, err
		}
		fmt.Fprintf(&callbacks, "\nstart_link() -> supervisor:start_link({local, %s}, ?MODULE, []).\n", f.Name.Name)
		fmt.Fprintf(&callbacks, "init(_) -> {ok, {{one_for_one, 1, 5}, [%s]}}.\n", strings.Join(children, ", "))
	default:
		// gen_server (existing behaviour) — unchanged body below.
		initFn := methodNamed(ms, "Init")
		if initFn == nil {
			return Result{}, fmt.Errorf("type %s has methods but no Init; not a recognized gen_server", typeName)
		}
		behaviour = "-behaviour(gen_server).\n"
		exports = append(exports, "init/1")
		// ... rest of the existing gen_server block, unchanged ...
	}
}
```

(Note: this task assumes Task 4's `Result` return type is NOT yet in place. To keep Task 2 self-contained, keep the current `(string, string, error)` signature: return `"", "", err` on error and build the string as today. Task 4 introduces `Result`. If executing strictly in order, use `return "", "", err` here and Task 4 rewrites these returns.)

Add the helpers at the end of the file:

```go
// isSupervisorInit reports whether fn is an `Init() []otp.Child` method,
// which marks a supervisor (as opposed to a gen_server's `Init() State`).
func isSupervisorInit(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	arr, ok := fn.Type.Results.List[0].Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	sel, ok := arr.Elt.(*ast.SelectorExpr)
	return ok && otpPkgIdent(sel.X) && sel.Sel.Name == "Child"
}

// supervisorChildren emits one Erlang child spec string per otp.Child in the
// supervisor Init's returned []otp.Child literal. Each child's Start is a
// package-qualified function value (pkg.Fn) mapped to the MFA {pkg, fn, []}.
func (em *emitter) supervisorChildren(fn *ast.FuncDecl) ([]string, error) {
	results, err := returnExprs(fn.Body)
	if err != nil {
		return nil, em.errorf(fn, "Init: %s", err)
	}
	if len(results) != 1 {
		return nil, em.errorf(fn, "supervisor Init must return one []otp.Child")
	}
	lit, ok := results[0].(*ast.CompositeLit)
	if !ok {
		return nil, em.errorf(fn, "supervisor Init must return an []otp.Child literal")
	}
	var specs []string
	for _, elt := range lit.Elts {
		child, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, em.errorf(elt, "supervisor child must be an otp.Child literal")
		}
		var id, mod, function string
		for _, e := range child.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				return nil, em.errorf(e, "otp.Child needs field: value")
			}
			switch kv.Key.(*ast.Ident).Name {
			case "ID":
				bl, ok := kv.Value.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return nil, em.errorf(kv.Value, "otp.Child ID must be a string literal")
				}
				id = strings.Trim(bl.Value, `"`)
			case "Start":
				sel, ok := kv.Value.(*ast.SelectorExpr)
				if !ok {
					return nil, em.errorf(kv.Value, "otp.Child Start must be a package-qualified function, e.g. echoserver.Start")
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return nil, em.errorf(kv.Value, "otp.Child Start must be pkg.Func")
				}
				mod = pkg.Name
				function = strings.ToLower(sel.Sel.Name)
			default:
				return nil, em.errorf(kv.Key, "unsupported otp.Child field %s", kv.Key.(*ast.Ident).Name)
			}
		}
		if id == "" || mod == "" {
			return nil, em.errorf(child, "otp.Child needs both ID and Start")
		}
		specs = append(specs, fmt.Sprintf("{%s, {%s, %s, []}, permanent, 5000, worker, [%s]}", id, mod, function, mod))
	}
	return specs, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestSupervisorBehaviour -v`
Expected: PASS. Then `go test ./internal/pkg/transpile/` — all existing gen_server tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): emit -behaviour(supervisor) from Init() []otp.Child"
```

---

### Task 3: Application behaviour emission

**Files:**
- Modify: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go` (add cases)

**Interfaces:**
- Consumes: `methodNamed`, `returnExprs`, `emitExpr`, `emitCall`.
- Produces: a behaviour branch recognizing an `App{ Start() otp.Pid; Stop() }` type → `-behaviour(application)` + `start/2` + `stop/1`; `emitCall` handles `otp.StartSupervisor(pkg.T{})` → `pkg:start_link()`.

- [ ] **Step 1: Write the failing test**

Add to `transpile_test.go`:

```go
func TestApplicationBehaviour(t *testing.T) {
	src := `package echoapp
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echosup"
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
`
	erl, mod, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echoapp" {
		t.Fatalf("mod = %q, want echoapp", mod)
	}
	for _, want := range []string{
		"-behaviour(application).",
		"start(_Type, _Args) -> echosup:start_link().",
		"stop(_State) -> ok.",
	} {
		if !strings.Contains(erl, want) {
			t.Fatalf("missing %q in:\n%s", want, erl)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestApplicationBehaviour -v`
Expected: FAIL — the `App` type has `Start`/`Stop` but no `Init`, so the default branch errors `type App has methods but no Init`.

- [ ] **Step 3: Write minimal implementation**

Add an application case to the behaviour switch in the methods loop, **before** the supervisor case:

```go
case methodNamed(ms, "Start") != nil && methodNamed(ms, "Stop") != nil:
	behaviour = "-behaviour(application).\n"
	exports = append(exports, "start/2", "stop/1")
	start := methodNamed(ms, "Start")
	results, err := returnExprs(start.Body)
	if err != nil {
		return "", "", em.errorf(start, "Start: %s", err)
	}
	if len(results) != 1 {
		return "", "", em.errorf(start, "application Start must return the supervisor pid")
	}
	sup, err := em.emitExpr(results[0])
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&callbacks, "\nstart(_Type, _Args) -> %s.\nstop(_State) -> ok.\n", sup)
```

Add the `StartSupervisor` handling in `emitCall`, next to the `StartServer` special case (before the generic arg-emission loop):

```go
if sel.Sel.Name == "StartSupervisor" {
	if len(c.Args) != 1 {
		return "", em.errorf(c, "otp.StartSupervisor takes one supervisor value")
	}
	lit, ok := c.Args[0].(*ast.CompositeLit)
	if !ok {
		return "", em.errorf(c, "otp.StartSupervisor requires a supervisor value, e.g. echosup.Sup{}")
	}
	selT, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return "", em.errorf(c, "otp.StartSupervisor argument must be pkg.Type{}")
	}
	pkg, ok := selT.X.(*ast.Ident)
	if !ok {
		return "", em.errorf(c, "otp.StartSupervisor argument must be pkg.Type{}")
	}
	return pkg.Name + ":start_link()", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestApplicationBehaviour -v`
Expected: PASS. Then full `go test ./internal/pkg/transpile/` — all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): emit -behaviour(application) from Start/Stop methods"
```

---

### Task 4: `Result`/`Module` return type, registered-name capture, and `.app` generation

**Files:**
- Modify: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go` (add cases)

**Interfaces:**
- Produces:
  - `type Result struct { Erl, Module, Behaviour string; Registered []string }`
  - `func Module(src string) (Result, error)` — the full emitter; `Behaviour` is one of `""`, `"gen_server"`, `"supervisor"`, `"application"`; `Registered` lists names from `otp.StartServer` calls.
  - `func File(src string) (string, string, error)` — thin back-compat wrapper: `r.Erl, r.Module, err`.
  - `func AppResource(app, vsn string, modules, registered []string) string` — the `.app` file body.
- Consumes: everything from Tasks 2–3.

- [ ] **Step 1: Write the failing test**

Add to `transpile_test.go`:

```go
func TestModuleReportsBehaviourAndRegistered(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServer("echo", State{}) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Module != "echoserver" || r.Behaviour != "gen_server" {
		t.Fatalf("got module=%q behaviour=%q", r.Module, r.Behaviour)
	}
	if len(r.Registered) != 1 || r.Registered[0] != "echo" {
		t.Fatalf("registered = %v, want [echo]", r.Registered)
	}
}

func TestAppResource(t *testing.T) {
	got := AppResource("echoapp", "0.2.3",
		[]string{"echoapp", "echosup", "echoserver"}, []string{"echo"})
	want := `{application, echoapp,
 [{description, "echoapp"},
  {vsn, "0.2.3"},
  {modules, [echoapp, echosup, echoserver]},
  {registered, [echo]},
  {applications, [kernel, stdlib]},
  {mod, {echoapp, []}}]}.
`
	if got != want {
		t.Fatalf("AppResource mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run 'TestModuleReportsBehaviourAndRegistered|TestAppResource' -v`
Expected: FAIL — `Module`, `Result`, `AppResource` undefined (build error).

- [ ] **Step 3: Write minimal implementation**

Add the `registered` field to `emitter`:

```go
type emitter struct {
	structs    map[string][]string
	fset       *token.FileSet
	registered []string
}
```

In the `StartServer` special case in `emitCall`, capture the name before returning:

```go
if sel.Sel.Name == "StartServer" {
	name, err := em.emitExpr(c.Args[0])
	if err != nil {
		return "", err
	}
	em.registered = append(em.registered, unquoteAtom(name))
	return fmt.Sprintf("gen_server:start_link({local, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
}
```

Add a `behaviourName` local in the emitter loop and set it in each branch (`"application"`, `"supervisor"`, `"gen_server"`). Declare `var behaviourName string` next to `var behaviour string`, and add `behaviourName = "supervisor"` / `= "application"` / `= "gen_server"` in the respective branches.

Rename the current `File` body to `Module`, changing its signature and all `return "", "", ...` to `return Result{}, ...`, and the final success return to:

```go
	return Result{Erl: b.String(), Module: f.Name.Name, Behaviour: behaviourName, Registered: em.registered}, nil
}
```

Add the `Result` type and back-compat `File` near the top (after the doc comment):

```go
// Result is the full outcome of transpiling one Go file: the Erlang source, the
// module name, the OTP behaviour ("", "gen_server", "supervisor", "application"),
// and the names it registers via otp.StartServer (for the .app resource).
type Result struct {
	Erl        string
	Module     string
	Behaviour  string
	Registered []string
}

// File transpiles src and returns the Erlang source and module name, discarding
// the richer Result fields. Retained for callers that only need the source.
func File(src string) (string, string, error) {
	r, err := Module(src)
	return r.Erl, r.Module, err
}
```

Add `AppResource`:

```go
// AppResource returns the Erlang .app resource body for an OTP application.
// applications is always [kernel, stdlib]; mod is {app, []}.
func AppResource(app, vsn string, modules, registered []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{application, %s,\n", app)
	fmt.Fprintf(&b, " [{description, %q},\n", app)
	fmt.Fprintf(&b, "  {vsn, %q},\n", vsn)
	fmt.Fprintf(&b, "  {modules, [%s]},\n", strings.Join(modules, ", "))
	fmt.Fprintf(&b, "  {registered, [%s]},\n", strings.Join(registered, ", "))
	fmt.Fprintf(&b, "  {applications, [kernel, stdlib]},\n")
	fmt.Fprintf(&b, "  {mod, {%s, []}}]}.\n", app)
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (new + all existing, incl. Tasks 2–3).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): add Module/Result, registered capture, AppResource"
```

---

### Task 5: `wm build` multi-file build with `.app` emission

**Files:**
- Modify: `internal/pkg/cli/cli.go`
- Test: `internal/pkg/cli/cli_test.go` (add cases)

**Interfaces:**
- Consumes: `transpile.Module`, `transpile.AppResource`.
- Produces: `buildCmd` accepts one or more `.go` paths; writes each `<out>/<mod>.erl`; when exactly one input has `Behaviour == "application"`, also writes `<out>/<app>.app` (vsn from `--vsn` or the `VERSION` file). New flag parser `parseVsnFlag`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pkg/cli/cli_test.go`:

```go
func TestBuildEmitsAppFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	app := write("app.go", `package echoapp
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echosup"
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
`)
	srv := write("srv.go", `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServer("echo", State{}) }
`)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	err := Run(context.Background(), []string{"build", app, srv, "--out", out, "--vsn", "0.2.3"},
		nil, &buf, &buf)
	if err != nil {
		t.Fatalf("build: %v\n%s", err, buf.String())
	}
	appFile := filepath.Join(out, "echoapp.app")
	data, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("expected %s: %v", appFile, err)
	}
	for _, want := range []string{
		"{application, echoapp,",
		`{vsn, "0.2.3"}`,
		"{modules, [echoapp, echoserver]}",
		"{registered, [echo]}",
		"{mod, {echoapp, []}}",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %s:\n%s", want, appFile, data)
		}
	}
}

func TestBuildSingleFileNoAppFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "srv.go")
	os.WriteFile(p, []byte(`package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
func Start() { otp.StartServer("echo", nil) }
`), 0o644)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	if err := Run(context.Background(), []string{"build", p, "--out", out}, nil, &buf, &buf); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "echoserver.erl")); err != nil {
		t.Fatalf("expected echoserver.erl: %v", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(out, "*.app")); len(entries) != 0 {
		t.Fatalf("no .app expected for a non-application build, got %v", entries)
	}
}
```

Ensure the test file imports `bytes`, `context`, `os`, `path/filepath`, `strings`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run 'TestBuildEmitsAppFile|TestBuildSingleFileNoAppFile' -v`
Expected: FAIL — `--vsn` unknown / multiple positional args rejected by the current `len(rest) != 1` guard.

- [ ] **Step 3: Write minimal implementation**

Replace `buildCmd` in `cli.go` with a multi-file version:

```go
// buildCmd transpiles each Go source path to Erlang, writing <out>/<module>.erl
// (out defaults to bin, overridable via --out) and refusing to overwrite. When
// exactly one input is an OTP application module, it also writes <out>/<app>.app,
// with vsn from --vsn or the VERSION file.
func buildCmd(args []string, stdout io.Writer) error {
	vsn, rest, err := parseVsnFlag(args)
	if err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wm build <path>... [--out DIR] [--vsn X]")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var modules, registered []string
	var appMod string
	for _, path := range rest {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			return err
		}
		dst := outPath(out, r.Module)
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
		}
		if err := os.WriteFile(dst, []byte(r.Erl), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, dst)
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			if appMod != "" {
				return fmt.Errorf("more than one application module (%s and %s)", appMod, r.Module)
			}
			appMod = r.Module
		}
	}
	if appMod != "" {
		if vsn == "" {
			vsn, err = readVersion()
			if err != nil {
				return err
			}
		}
		appFile := filepath.Join(out, appMod+".app")
		body := transpile.AppResource(appMod, vsn, modules, registered)
		if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, appFile)
	}
	return nil
}

// parseVsnFlag pulls an optional --vsn X (or --vsn=X); empty if absent.
func parseVsnFlag(args []string) (vsn string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--vsn":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--vsn requires a value")
			}
			vsn = args[i+1]
			i++
		case strings.HasPrefix(a, "--vsn="):
			vsn = strings.TrimPrefix(a, "--vsn=")
		default:
			rest = append(rest, a)
		}
	}
	return vsn, rest, nil
}

// readVersion reads and trims the project VERSION file in the working directory.
func readVersion() (string, error) {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "", fmt.Errorf("no --vsn given and cannot read VERSION: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS (new + existing cli tests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): multi-file wm build with .app emission"
```

---

### Task 6: Fixtures and ladder rungs IV.1–IV.4

**Files:**
- Create: `testdata/otpapp/go/echoapp/main.go`, `testdata/otpapp/go/echosup/main.go`, `testdata/otpapp/go/echoserver/main.go`, `testdata/otpapp/go/echoclient/main.go`
- Create: `testdata/otpapp/erlang/echoapp.erl`, `testdata/otpapp/erlang/echosup.erl`, `testdata/otpapp/erlang/echoserver.erl`, `testdata/otpapp/erlang/echoclient.erl`, `testdata/otpapp/erlang/echoapp.app`
- Create: `internal/pkg/ladder/ladder_otpapp_integration_test.go`

**Interfaces:**
- Consumes: `transpile.Module`, `transpile.AppResource`, `erlang.NewLayout`, `erlang.DefaultVersion`.
- Produces: integration tests `TestRungIV1_ErlangToErlang` … `TestRungIV4_BothWintermute`; helpers `runOtpApp(t, serverErls []string, appFile, clientErl string) string` and `transpileApp(t, dir string) (erls []string, appFile string)`.

- [ ] **Step 1: Write the Go fixtures**

`testdata/otpapp/go/echoapp/main.go`:

```go
package echoapp

import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/otpapp/go/echosup"
)

type App struct{}

func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
```

`testdata/otpapp/go/echosup/main.go`:

```go
package echosup

import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/otpapp/go/echoserver"
)

type Sup struct{}

func (Sup) Init() []otp.Child {
	return []otp.Child{{ID: "echo", Start: echoserver.Start}}
}
```

`testdata/otpapp/go/echoserver/main.go` (copy of the 0.2.2 genserver server):

```go
package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type State struct{ Count int }

func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) {
	return Req, State{Count: s.Count + 1}
}

func Start() { otp.StartServer("echo", State{}) }
```

`testdata/otpapp/go/echoclient/main.go` (copy of the 0.2.2 genserver client):

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
```

- [ ] **Step 2: Write the golden Erlang fixtures**

`testdata/otpapp/erlang/echoapp.erl`:

```erlang
-module(echoapp).
-behaviour(application).
-export([start/2, stop/1]).

start(_Type, _Args) -> echosup:start_link().
stop(_State) -> ok.
```

`testdata/otpapp/erlang/echosup.erl`:

```erlang
-module(echosup).
-behaviour(supervisor).
-export([start_link/0, init/1]).

start_link() -> supervisor:start_link({local, echosup}, ?MODULE, []).
init(_) -> {ok, {{one_for_one, 1, 5},
                 [{echo, {echoserver, start, []}, permanent, 5000, worker, [echoserver]}]}}.
```

`testdata/otpapp/erlang/echoserver.erl`:

```erlang
-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

init(_) -> {ok, {state, 0}}.
handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.

start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).
```

`testdata/otpapp/erlang/echoclient.erl`:

```erlang
-module(echoclient).
-export([main/0]).

main() -> io:format("~s~n", [gen_server:call(echo, <<"hello">>)]).
```

`testdata/otpapp/erlang/echoapp.app`:

```erlang
{application, echoapp,
 [{description, "echoapp"},
  {vsn, "0.2.3"},
  {modules, [echoapp, echosup, echoserver]},
  {registered, [echo]},
  {applications, [kernel, stdlib]},
  {mod, {echoapp, []}}]}.
```

- [ ] **Step 3: Write the failing integration test**

`internal/pkg/ladder/ladder_otpapp_integration_test.go`:

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
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// runOtpApp compiles the app/sup/server .erl files, places the .app on the code
// path, boots application:start(echoapp), runs the client, and halts.
func runOtpApp(t *testing.T, serverErls []string, appFile, clientErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	for _, src := range append(append([]string{}, serverErls...), clientErl) {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}
	data, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "echoapp.app"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	eval := "application:start(echoapp), echoclient:main(), init:stop()."
	out, err := exec.Command(l.Erl(), "-noshell", "-pa", work, "-eval", eval).CombinedOutput()
	if err != nil {
		t.Fatalf("erl: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// transpileApp transpiles the three server-side Go fixtures into <dir> and
// generates echoapp.app there, returning the .erl paths and the .app path.
func transpileApp(t *testing.T, dir string) ([]string, string) {
	t.Helper()
	goFiles := []string{
		"../../../testdata/otpapp/go/echoapp/main.go",
		"../../../testdata/otpapp/go/echosup/main.go",
		"../../../testdata/otpapp/go/echoserver/main.go",
	}
	var erls, modules, registered []string
	var appMod string
	for _, gf := range goFiles {
		src, err := os.ReadFile(gf)
		if err != nil {
			t.Fatal(err)
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, r.Module+".erl")
		if err := os.WriteFile(p, []byte(r.Erl), 0o644); err != nil {
			t.Fatal(err)
		}
		erls = append(erls, p)
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			appMod = r.Module
		}
	}
	appFile := filepath.Join(dir, appMod+".app")
	body := transpile.AppResource(appMod, "0.2.3", modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return erls, appFile
}

func erlServer() []string {
	return []string{
		"../../../testdata/otpapp/erlang/echoapp.erl",
		"../../../testdata/otpapp/erlang/echosup.erl",
		"../../../testdata/otpapp/erlang/echoserver.erl",
	}
}

const erlAppFile = "../../../testdata/otpapp/erlang/echoapp.app"
const erlClient = "../../../testdata/otpapp/erlang/echoclient.erl"

func TestRungIV1_ErlangToErlang(t *testing.T) {
	got := runOtpApp(t, erlServer(), erlAppFile, erlClient)
	if got != "hello" {
		t.Fatalf("rung IV.1 = %q, want hello", got)
	}
}

func TestRungIV2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/otpapp/go/echoclient/main.go", dir)
	got := runOtpApp(t, erlServer(), erlAppFile, client)
	if got != "hello" {
		t.Fatalf("rung IV.2 = %q, want hello", got)
	}
}

func TestRungIV3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpileApp(t, dir)
	got := runOtpApp(t, erls, appFile, erlClient)
	if got != "hello" {
		t.Fatalf("rung IV.3 = %q, want hello", got)
	}
}

func TestRungIV4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpileApp(t, dir)
	client := transpileToErl(t, "../../../testdata/otpapp/go/echoclient/main.go", dir)
	got := runOtpApp(t, erls, appFile, client)
	if got != "hello" {
		t.Fatalf("rung IV.4 = %q, want hello", got)
	}
}
```

(`transpileToErl` is reused from `ladder_integration_test.go`, same package + build tag.)

- [ ] **Step 4: Run the integration test to verify it passes**

Run: `go test -tags integration ./internal/pkg/ladder/ -run 'TestRungIV' -v`
Expected: PASS for IV.1–IV.4 (each prints `hello`). Requires local OTP at `~/.local/erlang/29.0.3`; if absent the tests Skip.

Then run the full ladder to confirm no regressions:
Run: `go test -tags integration ./internal/pkg/ladder/ -v`
Expected: all rungs 1–4, II.1–II.4, III.1–III.4, IV.1–IV.4 PASS.

- [ ] **Step 5: Commit**

```bash
git add testdata/otpapp internal/pkg/ladder/ladder_otpapp_integration_test.go
git commit -s -m "test(ladder): OTP application rungs IV.1–IV.4 on real OTP"
```

---

## Final verification gate (before merge)

- [ ] `go build -o bin/wm ./cmd/wm` succeeds.
- [ ] `go test ./...` all green (unit suite, stdlib only).
- [ ] `go test -tags integration ./internal/pkg/ladder/` — all 16 rungs PASS on OTP 29.0.3.
- [ ] `govulncheck ./...` and `gitleaks detect` clean; `gosec ./...` unchanged at the accepted dual-use findings.
- [ ] Copilot review gate on the staged diff before any github-bound push.

## Self-review notes

- **Spec coverage:** application convention → Task 3; supervisor convention → Task 2; gen_server/client reuse → Task 6 fixtures; `.app` fields (vsn/registered/modules/mod/applications) → Tasks 4 (generation) + 5 (wiring); markers → Task 1; multi-file `wm build` → Task 5; rungs IV.1–IV.4 → Task 6.
- **Deferred (roadmap, not in this plan):** supervisor strategy/restart selection, multiple children, nested supervisors, richer `.app` (deps/env/start_phases). Values hardcoded per Global Constraints.
- **Type consistency:** `Result{Erl, Module, Behaviour, Registered}`, `Module(src)`, `AppResource(app, vsn, modules, registered)`, `isSupervisorInit`, `supervisorChildren`, `runOtpApp`, `transpileApp` used identically wherever referenced.
- **Ordering caveat:** Task 2 is written against the current `(string, string, error)` signature; Task 4 introduces `Result`/`Module` and rewrites those returns. Execute in order.
