# Wintermute 0.2.2 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Express the echo as a `-behaviour(gen_server)` — a Go type `State` with `Init`/`HandleCall` + state — proving Wintermute's gen_server transpilation is interchangeable with hand-written Erlang.

**Architecture:** Extend `internal/pkg/transpile` to process methods (`fn.Recv != nil`, today skipped): a type with `Init`+`HandleCall` becomes a gen_server, with the transpiler supplying the OTP callback arities (`init/1`, `handle_call/3`), the receiver-derived state head-pattern, and the reply/ok tuples. Small orthogonal primitives (int literals, `+`, type-assert strip, `StartServer`/`Call` markers) land first, then the callback emission, then fixtures and a single-node ladder (step III) via the existing `runEcho`.

**Tech Stack:** Go stdlib only (`go/ast`, `go/token`, `testing`); Erlang/OTP 29.0.3 (`gen_server`).

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute`.
- **Stdlib only.** No third-party modules.
- **Never-silent-wrong.** The transpiler errors on anything outside its subset.
- **Guiding principle — Wintermute adapts to Erlang, no hidden automatisms:** identifiers that become Erlang **variables** (struct fields, callback params, bound vars) are written **uppercase-leading** in the Go source and lowercase is **rejected** (not auto-capitalized); identifiers that become **atoms** (func/module/registered names) are lowercased. A receiver name that only destructures away in a head pattern is exempt.
- **main() → run()**; all logic in `internal/pkg/`.
- **VERSION = `0.2.2`** (already bumped and committed).
- **Scope:** standalone single-node gen_server, stateful (Count). Supervisor/Application/deployment/cross-node/`handle_cast`/`handle_info`/native-Erlang interop are deferred.
- **Commit style:** conventional commits; sign off (`git commit -s`); trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Suite stays green:** `go test ./...` after every task. Step-III integration rungs (`go test -tags integration ./internal/pkg/ladder/`) pass on OTP 29.0.3 before merge; existing rungs 1–4 and II.1–II.4 stay green.

---

## Task 1: `otp.StartServer` / `otp.Call` markers

**Files:**
- Modify: `pkg/otp/otp.go`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: `otp.StartServer(name string, init any)` and `otp.Call(name string, req any) any` — transpile-only markers that panic natively via `transpileOnly`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/otp/otp_test.go`:

```go
func TestGenServerMarkersPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"StartServer", func() { StartServer("echo", nil) }},
		{"Call", func() { _ = Call("echo", "hi") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if !strings.Contains(msg, "wm build") || !strings.Contains(msg, tc.name) {
					t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
				}
			}()
			tc.call()
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/ -run TestGenServerMarkersPanic -v`
Expected: FAIL — `StartServer`/`Call` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `pkg/otp/otp.go`, after the `WhereisGlobal` marker:

```go
func StartServer(name string, init any) { transpileOnly("StartServer") }  // -> gen_server:start_link({local,name}, ?MODULE, [], [])
func Call(name string, req any) any      { transpileOnly("Call"); return nil } // -> gen_server:call(name, Req)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/otp/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/
git commit -s -m "feat(otp): add StartServer/Call gen_server markers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Integer literals + `+` binary expression

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `emitExpr` handles `*ast.BasicLit` of `token.INT` (→ the digits) and `*ast.BinaryExpr` with `token.ADD` (→ `L + R`); other operators error.

- [ ] **Step 1: Write the failing tests (white-box)**

Add to `internal/pkg/transpile/transpile_test.go` (ensure `go/ast`, `go/token` are imported):

```go
func TestEmitExpr_IntAndAdd(t *testing.T) {
	em := &emitter{structs: map[string][]string{}}
	// Count + 1
	expr := &ast.BinaryExpr{
		X:  &ast.Ident{Name: "Count"},
		Op: token.ADD,
		Y:  &ast.BasicLit{Kind: token.INT, Value: "1"},
	}
	got, err := em.emitExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Count + 1" {
		t.Fatalf("got %q, want %q", got, "Count + 1")
	}
}

func TestEmitExpr_NonAddBinaryErrors(t *testing.T) {
	em := &emitter{structs: map[string][]string{}}
	expr := &ast.BinaryExpr{X: &ast.Ident{Name: "A"}, Op: token.SUB, Y: &ast.Ident{Name: "B"}}
	if _, err := em.emitExpr(expr); err == nil {
		t.Fatal("want error for unsupported binary operator, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TestEmitExpr_(IntAndAdd|NonAddBinaryErrors)' -v`
Expected: FAIL — INT literal hits "unsupported literal"; `*ast.BinaryExpr` hits the `default` "unsupported expression".

- [ ] **Step 3: Write minimal implementation**

In `emitExpr`, extend the `*ast.BasicLit` case to accept INT, and add a `*ast.BinaryExpr` case:

```go
	case *ast.BasicLit:
		switch ex.Kind {
		case token.STRING:
			return "<<" + ex.Value + ">>", nil // ex.Value keeps the quotes
		case token.INT:
			return ex.Value, nil
		}
		return "", em.errorf(ex, "unsupported literal: %s", ex.Value)
	case *ast.BinaryExpr:
		if ex.Op != token.ADD {
			return "", em.errorf(ex, "unsupported binary operator %s (only + in the gen_server subset)", ex.Op)
		}
		l, err := em.emitExpr(ex.X)
		if err != nil {
			return "", err
		}
		r, err := em.emitExpr(ex.Y)
		if err != nil {
			return "", err
		}
		return l + " + " + r, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (existing string-literal tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): support int literals and + binary expr

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Type-assert strip + `otp.Call` → `gen_server:call`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr`, `emitCall`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `Call` marker (Task 1).
- Produces: `emitExpr` handles `*ast.TypeAssertExpr` (emits the inner expression — the assertion is Go-only); `emitCall` maps `Call` → `gen_server:call(<atom>, Req)`.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_GenServerCall(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `io:format("~s~n", [gen_server:call(echo, <<"hello">>)])`) {
		t.Fatalf("got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_GenServerCall -v`
Expected: FAIL — `*ast.TypeAssertExpr` hits `default`; `Call` is an unsupported otp call.

- [ ] **Step 3: Write minimal implementation**

Add a `*ast.TypeAssertExpr` case to `emitExpr` (before `default`):

```go
	case *ast.TypeAssertExpr:
		// x.(T) outside a receive: Erlang is dynamically typed, so the
		// assertion is Go-only — emit the inner expression.
		return em.emitExpr(ex.X)
```

Add a `Call` case to `emitCall`'s `switch sel.Sel.Name` (after `WhereisGlobal`):

```go
	case "Call":
		return fmt.Sprintf("gen_server:call(%s, %s)", unquoteAtom(args[0]), args[1]), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): strip type-assert and map Call to gen_server:call

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: `otp.StartServer` → `gen_server:start_link`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitCall`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `StartServer` marker (Task 1).
- Produces: `emitCall` special-cases `StartServer` (like `Spawn`, before the general arg loop) → `gen_server:start_link({local, <atom>}, ?MODULE, [], [])`, ignoring the second (type-marker) argument.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_StartServer(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func Start() { otp.StartServer("echo", State{}) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).") {
		t.Fatalf("got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_StartServer -v`
Expected: FAIL — `StartServer` is unsupported; also the general arg loop would try to emit `State{}` (a composite literal omitting `Count`) and error.

- [ ] **Step 3: Write minimal implementation**

In `emitCall`, add a `StartServer` special-case right after the `Spawn` special-case (before the general `args` loop), so the type-marker second arg is never emitted:

```go
	// otp.StartServer("echo", State{}) — the second arg is a type marker (which
	// gen_server type carries the callbacks); the current module IS the
	// gen_server (?MODULE), so it is not emitted as a runtime value.
	if sel.Sel.Name == "StartServer" {
		name, err := em.emitExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("gen_server:start_link({local, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): map StartServer to gen_server:start_link

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: gen_server behaviour detection + `init/1`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`File`, plus new helpers)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `File` recognizes a type with an `Init` method as a gen_server, emitting `-behaviour(gen_server).`, exporting `init/1`, and emitting `init(_) -> {ok, <state>}.` from the `Init` method body. Adds helpers `receiverTypeName(fn *ast.FuncDecl) string` and `methodNamed(ms []*ast.FuncDecl, name string) *ast.FuncDecl`.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_GenServerInit(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
var _ = otp.Self
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-behaviour(gen_server).",
		"init(_) -> {ok, {state, 0}}.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
```

(`var _ = otp.Self` keeps the `otp` import used so the source is valid Go; the transpiler ignores `var` decls.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_GenServerInit -v`
Expected: FAIL — methods are skipped (`fn.Recv != nil`), so no behaviour header or `init/1` is emitted.

- [ ] **Step 3: Write minimal implementation**

Add helpers near the bottom of `transpile.go`:

```go
// receiverTypeName returns the name of fn's receiver type (value or pointer),
// or "" if fn has no receiver.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// methodNamed returns the method named name from ms, or nil.
func methodNamed(ms []*ast.FuncDecl, name string) *ast.FuncDecl {
	for _, m := range ms {
		if m.Name.Name == name {
			return m
		}
	}
	return nil
}

// returnExprs returns the expressions of the single return statement in body,
// or an error if the body is not exactly one return statement.
func returnExprs(body *ast.BlockStmt) ([]ast.Expr, error) {
	if body == nil || len(body.List) != 1 {
		return nil, fmt.Errorf("callback body must be a single return statement")
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok {
		return nil, fmt.Errorf("callback body must be a return statement")
	}
	return ret.Results, nil
}
```

In `File`, after the struct-collection loop and before building the nullary-func output, collect methods and emit gen_server callbacks. Insert this block (it appends to `exports` and writes into a `callbacks` builder that is written to the module after `bodies`):

```go
	// Collect methods by receiver type; a type with an Init method is a gen_server.
	methods := map[string][]*ast.FuncDecl{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		rt := receiverTypeName(fn)
		methods[rt] = append(methods[rt], fn)
	}
	var behaviour string
	var callbacks strings.Builder
	for typeName, ms := range methods {
		initFn := methodNamed(ms, "Init")
		if initFn == nil {
			return "", "", fmt.Errorf("type %s has methods but no Init; not a recognized gen_server", typeName)
		}
		behaviour = "-behaviour(gen_server).\n"
		exports = append(exports, "init/1")
		results, err := returnExprs(initFn.Body)
		if err != nil {
			return "", "", em.errorf(initFn, "Init: %s", err)
		}
		state, err := em.emitExpr(results[0])
		if err != nil {
			return "", "", err
		}
		fmt.Fprintf(&callbacks, "\ninit(_) -> {ok, %s}.\n", state)
	}
```

Then include `behaviour` and `callbacks` in the module output. Change the final assembly:

```go
	var b strings.Builder
	fmt.Fprintf(&b, "-module(%s).\n", f.Name.Name)
	b.WriteString(behaviour)
	fmt.Fprintf(&b, "-export([%s]).\n", strings.Join(exports, ", "))
	b.WriteString(bodies.String())
	b.WriteString(callbacks.String())
	return b.String(), f.Name.Name, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS. The single-node/dist echo fixtures (no methods) are unaffected — `methods` is empty, `behaviour` stays "".

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): detect gen_server and emit init/1

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: `handle_call/3`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`File` gen_server block)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: the gen_server block (Task 5), `receiverTypeName`/`methodNamed`/`returnExprs`, binary-expr (Task 2).
- Produces: `File` also emits `handle_call/3` — `HandleCall(Req string) (string, State)` → `handle_call(Req, _From, {state, Count}) -> {reply, Reply, NewState}.`, with the param as an uppercase-validated Erlang variable and the receiver destructured to the state head-pattern.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_GenServerHandleCall(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) {
	return Req, State{Count: s.Count + 1}
}
var _ = otp.Self
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"handle_call/3",
		"handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GenServerLowercaseParamErrors(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(req string) (string, State) { return req, s }
var _ = otp.Self
`
	if _, _, err := File(src); err == nil {
		t.Fatal("want error for lowercase-leading callback param, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_GenServerHandleCall -v`
Expected: FAIL — only `init/1` is emitted; no `handle_call/3`.

- [ ] **Step 3: Write minimal implementation**

In `File`'s gen_server `for typeName, ms := range methods` loop, after emitting `init/1`, emit `handle_call/3` when a `HandleCall` method exists. The receiver state pattern binds all fields of the receiver type (like `receiveHead`); the param is uppercase-validated:

```go
		if hc := methodNamed(ms, "HandleCall"); hc != nil {
			exports = append(exports, "handle_call/3")
			// Param -> uppercase Erlang variable (guiding principle: reject lowercase).
			if hc.Type.Params == nil || len(hc.Type.Params.List) != 1 || len(hc.Type.Params.List[0].Names) != 1 {
				return "", "", em.errorf(hc, "HandleCall must take exactly one parameter")
			}
			param := hc.Type.Params.List[0].Names[0].Name
			if !token.IsExported(param) {
				return "", "", em.errorf(hc, "HandleCall parameter %s is lowercase-leading; Erlang variables must be uppercase", param)
			}
			// Receiver state head-pattern: {state, F1, F2, ...} binding all fields.
			statePat := []string{strings.ToLower(typeName)}
			statePat = append(statePat, em.structs[typeName]...)
			pattern := "{" + strings.Join(statePat, ", ") + "}"
			// Body: return Reply, NewState -> {reply, Reply, NewState}.
			results, err := returnExprs(hc.Body)
			if err != nil {
				return "", "", em.errorf(hc, "HandleCall: %s", err)
			}
			if len(results) != 2 {
				return "", "", em.errorf(hc, "HandleCall must return (reply, state)")
			}
			reply, err := em.emitExpr(results[0])
			if err != nil {
				return "", "", err
			}
			next, err := em.emitExpr(results[1])
			if err != nil {
				return "", "", err
			}
			fmt.Fprintf(&callbacks, "handle_call(%s, _From, %s) -> {reply, %s, %s}.\n", param, pattern, reply, next)
		}
```

(`s.Count` in the body emits via the existing `*ast.SelectorExpr` case → `Count`, which is bound by the head pattern; `s.Count + 1` uses Task 2's binary-expr.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS, including the lowercase-param rejection.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): emit gen_server handle_call/3

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: gen_server fixtures + golden tests

**Files:**
- Create: `testdata/genserver/go/echoserver/main.go`, `testdata/genserver/go/echoclient/main.go`
- Create: `testdata/genserver/erlang/echoserver.erl`, `testdata/genserver/erlang/echoclient.erl`
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: all transpiler capabilities (Tasks 1–6).
- Produces: the four fixtures the ladder (Task 8) compiles and runs.

- [ ] **Step 1: Write the failing golden tests**

```go
func TestFile_GoldenGenServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/genserver/go/echoserver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-module(echoserver).",
		"-behaviour(gen_server).",
		"init(_) -> {ok, {state, 0}}.",
		"handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.",
		"start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GoldenGenServerClient(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/genserver/go/echoclient/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `main() -> io:format("~s~n", [gen_server:call(echo, <<"hello">>)]).`) {
		t.Fatalf("got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TestFile_GoldenGenServer' -v`
Expected: FAIL — fixture files do not exist.

- [ ] **Step 3: Create the fixtures**

`testdata/genserver/go/echoserver/main.go`:

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

`testdata/genserver/go/echoclient/main.go`:

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
```

`testdata/genserver/erlang/echoserver.erl`:

```erlang
-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

init(_) -> {ok, {state, 0}}.
handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.

start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).
```

`testdata/genserver/erlang/echoclient.erl`:

```erlang
-module(echoclient).
-export([main/0]).

main() -> io:format("~s~n", [gen_server:call(echo, <<"hello">>)]).
```

- [ ] **Step 4: Run tests to verify they pass, and vet the Go fixtures**

Run: `go test ./internal/pkg/transpile/ -run 'TestFile_GoldenGenServer' -v && go vet ./testdata/genserver/go/...`
Expected: PASS; `go vet` clean (fixtures are valid Go).

- [ ] **Step 5: Commit**

```bash
git add testdata/genserver/ internal/pkg/transpile/
git commit -s -m "test(transpile): add gen_server fixtures + golden tests

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Ladder rungs III.1 – III.4 (single-node gen_server)

**Files:**
- Create: `internal/pkg/ladder/ladder_genserver_integration_test.go`
- Test: same file (`//go:build integration`)

**Interfaces:**
- Consumes: `runEcho(t, serverErl, clientErl)` and `transpileToErl(t, goPath, dir)` from `internal/pkg/ladder/ladder_integration_test.go` (same package). The gen_server fixtures (Task 7).

- [ ] **Step 1: Write the failing tests**

Create `internal/pkg/ladder/ladder_genserver_integration_test.go`:

```go
//go:build integration

package ladder

import (
	"path/filepath"
	"testing"
)

// Rung III proves gen_server interchangeability on a single node: runEcho boots
// echoserver:start() (which starts the gen_server, registered locally as echo)
// then echoclient:main() (which gen_server:call's it), all in one BEAM node.

func TestRungIII1_ErlangToErlang(t *testing.T) {
	got := runEcho(t,
		filepath.FromSlash("../../../testdata/genserver/erlang/echoserver.erl"),
		filepath.FromSlash("../../../testdata/genserver/erlang/echoclient.erl"))
	if got != "hello" {
		t.Fatalf("rung III.1 = %q, want %q", got, "hello")
	}
}

func TestRungIII2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/genserver/go/echoclient/main.go", dir)
	got := runEcho(t, "../../../testdata/genserver/erlang/echoserver.erl", client)
	if got != "hello" {
		t.Fatalf("rung III.2 = %q", got)
	}
}

func TestRungIII3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/genserver/go/echoserver/main.go", dir)
	got := runEcho(t, server, "../../../testdata/genserver/erlang/echoclient.erl")
	if got != "hello" {
		t.Fatalf("rung III.3 = %q", got)
	}
}

func TestRungIII4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/genserver/go/echoserver/main.go", dir)
	client := transpileToErl(t, "../../../testdata/genserver/go/echoclient/main.go", dir)
	got := runEcho(t, server, client)
	if got != "hello" {
		t.Fatalf("rung III.4 = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests on real Erlang**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungIII -v`
Expected: PASS for III.1–III.4 (each `hello`) on OTP 29.0.3. Confirm they RUN (not SKIP). If Erlang is absent, provision first (`./bin/wm erlang install`).

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/ladder/ladder_genserver_integration_test.go
git commit -s -m "test(ladder): rungs III.1-III.4 (single-node gen_server)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Verification gate

**Files:** none (verification only).

- [ ] **Step 1: Full unit suite + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: all green (gen_server golden tests, otp marker tests included).

- [ ] **Step 2: Real full integration ladder** (memory: green units are not enough)

```bash
go test -tags integration ./internal/pkg/ladder/ -v
```
Expected: rungs 1–4 (single-node echo), II.1–II.4 (distributed), AND III.1–III.4 (gen_server) all PASS on real OTP 29.0.3. This is the gen_server-interchangeability thesis proof.

- [ ] **Step 3: Security tooling** (surface changed little; confirm no new findings)

```bash
govulncheck ./...
gosec ./...
gitleaks detect
```
Expected: no new findings beyond the 10 accepted 0.2.0 dual-use patterns. Triage/fix anything new.

- [ ] **Step 4: Update HANDOVER.md**

Mark 0.2.2 complete; note the next 0.2.x step (0.2.3 deployment — bring the service into a running node/cluster; supervisor/application) and the tracked native-Erlang-interop question. Commit.

---

## Self-Review

**Spec coverage:** Guiding principle (uppercase params rejected-not-capitalized) → T6 (param check) + the field guard already in `File`. Capabilities: method decls → T5; behaviour detection → T5; callback signatures init/1 → T5, handle_call/3 → T6; function params → T6; receiver destructuring → T6; field access → existing `SelectorExpr` (exercised by T6); binary expr → T2; multi-value return → T6 (and Init's single return → T5); type-assert strip → T3; markers StartServer → T4, Call → T3, otp side → T1. Fixtures → T7. Rungs III.1–III.4 → T8. Testing (unit + gated integration) → T1–T7 (unit), T8 (integration), T9 (gate). All spec sections covered.

**Placeholder scan:** No TBD/TODO. `Reply`/`NewState` in the T6 Interfaces line are descriptive (the concrete emitted values `Req`/`{state, Count + 1}` are in the test). `%s` are real format verbs. Fixtures and emit code are complete.

**Type consistency:** `otp.StartServer(name string, init any)` / `otp.Call(name, req any) any` (T1) are consumed by T3/T4 emit cases and T7 fixtures identically. `receiverTypeName`/`methodNamed`/`returnExprs` defined in T5, reused in T6. The state tuple tag is `strings.ToLower(typeName)` = `state` throughout (T5 init pattern, T6 head pattern, both matching the fixtures' `{state, ...}`).

**Simplification noted:** T6 binds *all* receiver fields in the head pattern (like `receiveHead`), not only used ones — for the single-field `Count` (used) this is identical to the spec's "unused → `_`". The `_`-for-unused refinement is deferred until multi-field state with unused fields appears (0.2.x backlog); flagged so it isn't mistaken for full spec coverage.

**Ordering note:** Primitives/markers (T1–T4) precede the callback emission (T5–T6); T6's `Count + 1` needs T2's binary-expr. Golden tests (T7) need T1–T6. Ladder (T8) needs fixtures (T7) and real Erlang (SKIPs without it — proven at the T9 gate).
