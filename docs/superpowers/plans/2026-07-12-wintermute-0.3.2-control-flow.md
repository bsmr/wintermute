# Wintermute 0.3.2 — Control Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add operators (arithmetic beyond `+`, comparison, boolean) and `if` → Erlang `case` to the transpiler, turning the flat function body into a value-yielding tree and making recursion useful.

**Architecture:** Extend the existing emitter in `internal/pkg/transpile/transpile.go`. Operators are an expression-level change (`emitExpr`). `if` is handled in `emitStmts` (tail position only), emitting a `case Cond of true -> …; false -> … end` whose false branch is the explicit else or — for a bare `if` — the continuation (statements after the `if`). Each branch is emitted in its own binding scope: `em.bound` is snapshotted (`maps.Clone`) and restored around each branch, so sibling clauses may reuse a name while an outer collision stays rejected.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`, `maps`). Erlang/OTP 29.0.3 `erlc`/`erl` for the runnable rung.

## Global Constraints

- **Stdlib only. No third-party modules.** (`maps.Clone` is stdlib; Go is 1.26.5.)
- **TDD, red → green:** write the failing test, watch it fail, then implement.
- **Module path is `go.muehmer.eu/wintermute`.**
- **Erlang-variable casing rule** stays: binder names must be uppercase-leading; reject lowercase, never auto-capitalize.
- **`bound-set-integration` invariant:** every new binding context must scope correctly. `if` branches are a new binding context — snapshot/restore `bound` so sibling branches reuse names freshly while outer collisions are rejected. Test both.
- **No transpiler automatism beyond a clean 1:1 Erlang mapping.** Deferred constructs (`switch`, `else if` chains, side-effect-only `if`, guards) must error, pointing at 0.3.3 where useful.
- **Deterministic output**; **build to `bin/`** (`go build -o bin/wm ./cmd/wm`).
- **Equality is exact:** `==` → `=:=`, `!=` → `=/=` (no numeric coercion).

Reference: spec at `docs/superpowers/specs/2026-07-12-wintermute-0.3.2-control-flow-design.md`.

Test commands: unit `go test ./internal/pkg/transpile/`; full `go test ./...`; integration `go test -tags integration ./internal/pkg/ladder/`.

---

### Task 1: Operators (binary table, unary `!`, parens, ParenExpr)

Replace the `+`-only `*ast.BinaryExpr` branch with a full operator table; add `*ast.UnaryExpr` (`!` → `not`) and `*ast.ParenExpr` (unwrap) cases; parenthesize an operand that is itself a binary expression so Go's grouping survives.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr` `*ast.BinaryExpr` case ~lines 431-443; add cases + helpers)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces:
  - `var binOp map[token.Token]string` — Go binary token → Erlang operator string.
  - `func (em *emitter) emitOperand(e ast.Expr) (string, error)` — emits `e`, wrapping it in parens iff the parenthesis-stripped `e` is a `*ast.BinaryExpr`.
  - `func unparen(e ast.Expr) ast.Expr` — strips `*ast.ParenExpr` layers.
  - `emitExpr` now handles `*ast.UnaryExpr` (`token.NOT`) and `*ast.ParenExpr`.

- [ ] **Step 1: Write the failing tests**

Add to `transpile_test.go`:

```go
func TestModule_Operators(t *testing.T) {
	cases := []struct{ goExpr, erl string }{
		{"A - B", "a() -> A - B."},
		{"A * B", "a() -> A * B."},
		{"A / B", "a() -> A div B."},
		{"A % B", "a() -> A rem B."},
		{"A == B", "a() -> A =:= B."},
		{"A != B", "a() -> A =/= B."},
		{"A < B", "a() -> A < B."},
		{"A <= B", "a() -> A =< B."},
		{"A > B", "a() -> A > B."},
		{"A >= B", "a() -> A >= B."},
		{"A && B", "a() -> A andalso B."},
		{"A || B", "a() -> A orelse B."},
	}
	for _, c := range cases {
		src := "package m\nfunc A(A, B int) int { return " + c.goExpr + " }"
		r, err := Module(src)
		if err != nil {
			t.Fatalf("%s: Module: %v", c.goExpr, err)
		}
		if !strings.Contains(r.Erl, c.erl) {
			t.Errorf("%s: want %q, got:\n%s", c.goExpr, c.erl, r.Erl)
		}
	}
}

func TestModule_UnaryNot(t *testing.T) {
	src := `package m
func F(A bool) bool { return !A }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "f(A) -> not A.") {
		t.Errorf("want not A, got:\n%s", r.Erl)
	}
}

func TestModule_PrecedenceParens(t *testing.T) {
	// A binary operand keeps its grouping; a single operator stays bare.
	src := `package m
func F(A, B, C int) int { return (A + B) * C }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "f(A, B, C) -> (A + B) * C.") {
		t.Errorf("want (A + B) * C, got:\n%s", r.Erl)
	}
}

func TestModule_UnsupportedOperatorRejected(t *testing.T) {
	src := `package m
func F(A, B int) int { return A << B }`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("want unsupported-operator error, got %v", err)
	}
}
```

Note: `A` is used as both function name (→ atom `a`) and a parameter name (→ Erlang `A`) — this is legal in the subset. The single-`+` behavior (`X + Y` bare) is already covered by existing tests; do not change them.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'Operators|UnaryNot|PrecedenceParens|UnsupportedOperatorRejected' -v`
Expected: FAIL — the current `*ast.BinaryExpr` case rejects everything but `+`, and there is no `*ast.UnaryExpr`/`*ast.ParenExpr` case.

- [ ] **Step 3: Add the operator table and helpers**

Add near the top of the file (after the imports / next to other package-level vars, or just before `emitExpr`):

```go
// binOp maps Go binary operators to their Erlang spelling. Equality is exact
// (=:= / =/=), matching Go's non-coercing == on ints and atoms.
var binOp = map[token.Token]string{
	token.ADD: "+", token.SUB: "-", token.MUL: "*",
	token.QUO: "div", token.REM: "rem",
	token.EQL: "=:=", token.NEQ: "=/=",
	token.LSS: "<", token.GTR: ">", token.LEQ: "=<", token.GEQ: ">=",
	token.LAND: "andalso", token.LOR: "orelse",
}

// unparen strips parenthesis layers from e.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// emitOperand emits e as an operand of a binary/unary operator, wrapping it in
// parentheses when (ignoring existing parens) it is itself a binary expression,
// so Go's grouping survives regardless of Erlang's operator precedence. A single
// operator stays bare (X + Y, not (X + Y)).
func (em *emitter) emitOperand(e ast.Expr) (string, error) {
	s, err := em.emitExpr(e)
	if err != nil {
		return "", err
	}
	if _, ok := unparen(e).(*ast.BinaryExpr); ok {
		return "(" + s + ")", nil
	}
	return s, nil
}
```

- [ ] **Step 4: Rewrite the `*ast.BinaryExpr` case and add `UnaryExpr`/`ParenExpr`**

Replace the current `*ast.BinaryExpr` case (the `if ex.Op != token.ADD { … } … return l + " + " + r, nil` block) with:

```go
	case *ast.BinaryExpr:
		op, ok := binOp[ex.Op]
		if !ok {
			return "", em.errorf(ex, "unsupported binary operator %s (0.3.3+)", ex.Op)
		}
		l, err := em.emitOperand(ex.X)
		if err != nil {
			return "", err
		}
		r, err := em.emitOperand(ex.Y)
		if err != nil {
			return "", err
		}
		return l + " " + op + " " + r, nil
	case *ast.UnaryExpr:
		if ex.Op != token.NOT {
			return "", em.errorf(ex, "unsupported unary operator %s", ex.Op)
		}
		x, err := em.emitOperand(ex.X)
		if err != nil {
			return "", err
		}
		return "not " + x, nil
	case *ast.ParenExpr:
		return em.emitExpr(ex.X)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'Operators|UnaryNot|PrecedenceParens|UnsupportedOperatorRejected' -v`
Expected: PASS all.

- [ ] **Step 6: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS — existing `+` tests still green (bare `X + Y` unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): full operator set (arithmetic, comparison, boolean, unary not)"
```

---

### Task 2: `if` → Erlang `case` (both forms, branch scoping)

Handle `*ast.IfStmt` in `emitStmts` (tail position only). Emit `case Cond of true -> then; false -> else end`, where the false branch is the explicit else block or — for a bare `if` — the continuation. Each branch is emitted in its own `bound` scope (snapshot/restore). Reject `else if`, `if` init, non-terminating branches, unreachable statements after a full if/else, a bare `if` with no continuation, and `if` outside tail position.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (add `maps` import; `emitStmts` loop; add `emitIf`, `emitBranch`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `em.emitExpr` (conditions), `em.emitStmts` (branch bodies), `em.bound`.
- Produces:
  - `func (em *emitter) emitIf(is *ast.IfStmt, cont []ast.Stmt) (string, error)` — emits the `case`; `cont` is the continuation (statements after the `if`), used as the false branch of a bare `if`.
  - `func (em *emitter) emitBranch(list []ast.Stmt) (string, error)` — emits a statement list as a value in a snapshotted/restored `bound` scope.

- [ ] **Step 1: Write the failing tests**

```go
func TestModule_IfElse(t *testing.T) {
	src := `package m
func Sign(N int) int {
	if N == 0 {
		return 0
	} else {
		return 1
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	for _, want := range []string{"case N =:= 0 of", "true -> 0", "false -> 1", "end"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q, got:\n%s", want, r.Erl)
		}
	}
}

func TestModule_BareIfBaseCase(t *testing.T) {
	src := `package m
func Fact(N int) int {
	if N == 0 {
		return 1
	}
	return N * Fact(N-1)
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	for _, want := range []string{"case N =:= 0 of", "true -> 1", "false -> N * fact(N - 1)"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q, got:\n%s", want, r.Erl)
		}
	}
}

func TestModule_BranchSiblingReuse(t *testing.T) {
	// Z bound fresh in both branches — legal (independent Erlang case-clause scopes).
	src := `package m
func F(N int) int {
	if N == 0 {
		Z := 1
		return Z
	}
	Z := 2
	return Z
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("want sibling reuse accepted, got %v", err)
	}
	if !strings.Contains(r.Erl, "true -> Z = 1") || !strings.Contains(r.Erl, "false -> Z = 2") {
		t.Errorf("got:\n%s", r.Erl)
	}
}

func TestModule_BranchOuterCollisionRejected(t *testing.T) {
	// Z collides with the parameter Z — rejected (outer binding visible in the branch).
	src := `package m
func F(Z int) int {
	if Z == 0 {
		Z := 1
		return Z
	}
	return Z
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want outer-collision rejection, got %v", err)
	}
}

func TestModule_ElseIfRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	} else if N == 1 {
		return 1
	}
	return 2
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "else-if") {
		t.Fatalf("want else-if rejection, got %v", err)
	}
}

func TestModule_BareIfNoContinuationRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "false branch") {
		t.Fatalf("want bare-if-no-continuation rejection, got %v", err)
	}
}

func TestModule_BareIfNonTerminatingThenRejected(t *testing.T) {
	// A bare-if then-branch that does not return would, in Go, fall through to
	// the continuation; an Erlang terminal case clause cannot express that, so
	// it must be rejected rather than silently returning the branch's value.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F(N int) int {
	if N == 0 {
		otp.Print("zero")
	}
	return N
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "fall through") {
		t.Fatalf("want non-terminating-then rejection, got %v", err)
	}
}

func TestModule_UnreachableAfterIfElseRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	} else {
		return 1
	}
	return 2
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("want unreachable rejection, got %v", err)
	}
}

func TestModule_IfNonTailRejected(t *testing.T) {
	// An if in the pre-receive slice is not in tail position (isTail=false).
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle(N int) int {
	if N == 0 {
		return 0
	}
	M := otp.Receive().(Msg)
	return M.X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "tail position") {
		t.Fatalf("want non-tail-if rejection, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'If|Branch' -v`  (catches all if/else/bare-if/branch-scoping/rejection tests)
Expected: FAIL — `*ast.IfStmt` currently reaches `emitStmt`'s `default` → `unsupported statement: *ast.IfStmt`.

- [ ] **Step 3: Add the `maps` import**

In the import block, add `"maps"` (keep imports grouped/sorted):

```go
import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Add `emitIf` and `emitBranch`**

Add these methods (e.g. right after `emitStmts`):

```go
// emitIf emits an if statement as an Erlang `case Cond of true -> …; false -> …
// end`. The false branch is the explicit else block, or — for a bare if — the
// continuation `cont` (statements following the if). Only if/else and bare-if
// are supported; else-if chains, an init clause, and a bare if with no
// continuation are rejected (0.3.3+).
func (em *emitter) emitIf(is *ast.IfStmt, cont []ast.Stmt) (string, error) {
	if is.Init != nil {
		return "", em.errorf(is, "if with an init statement is unsupported (0.3.3+)")
	}
	if _, ok := is.Else.(*ast.IfStmt); ok {
		return "", em.errorf(is, "else-if chains are unsupported (0.3.3+); use a nested if")
	}
	cond, err := em.emitExpr(is.Cond)
	if err != nil {
		return "", err
	}
	then, err := em.emitBranch(is.Body.List)
	if err != nil {
		return "", err
	}
	var els string
	switch e := is.Else.(type) {
	case *ast.BlockStmt:
		if len(cont) != 0 {
			return "", em.errorf(cont[0], "unreachable statement after a terminating if/else")
		}
		els, err = em.emitBranch(e.List)
	case nil:
		if len(cont) == 0 {
			return "", em.errorf(is, "a bare if needs a following value (the case's false branch)")
		}
		if !terminates(is.Body.List) {
			return "", em.errorf(is, "the then-branch of a bare if must end in a return; otherwise it would fall through to the continuation, which a terminal Erlang case clause cannot express")
		}
		els, err = em.emitBranch(cont)
	default:
		return "", em.errorf(is, "unsupported else form")
	}
	if err != nil {
		return "", err
	}
	return "case " + cond + " of\n" +
		indent("true -> "+then) + ";\n" +
		indent("false -> "+els) + "\nend", nil
}

// terminates reports whether a statement list ends in a construct that yields
// the function's value and does not fall through: a return, or an if/else whose
// both branches terminate. A bare if (no else) falls through, so it does not
// terminate. Used to reject a bare-if then-branch that would fall through to the
// continuation (Go semantics) but be emitted as a terminal case clause (Erlang).
func terminates(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	switch s := list[len(list)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.IfStmt:
		els, ok := s.Else.(*ast.BlockStmt)
		return ok && terminates(s.Body.List) && terminates(els.List)
	default:
		return false
	}
}

// emitBranch emits a case-clause body (an if/else block or a bare-if
// continuation) as a value-yielding Erlang sequence in its own binding scope:
// bound is snapshotted and restored, so a name bound here does not leak to a
// sibling branch (Erlang case clauses are independent scopes), while outer
// bindings stay visible and their collisions stay rejected.
func (em *emitter) emitBranch(list []ast.Stmt) (string, error) {
	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()
	return em.emitStmts(list, true)
}
```

- [ ] **Step 5: Wire `IfStmt` into `emitStmts`**

In `emitStmts`, add an `*ast.IfStmt` branch at the TOP of the loop body (before the `*ast.ReturnStmt` check). Also refine the early-return message to mention `if`. The loop becomes:

```go
	for i, s := range list {
		if is, ok := s.(*ast.IfStmt); ok {
			if !isTail {
				return "", em.errorf(is, "control flow (if) is only supported in tail position")
			}
			e, err := em.emitIf(is, list[i+1:])
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			return strings.Join(parts, ",\n"), nil // an if consumes the rest of the sequence
		}
		if ret, ok := s.(*ast.ReturnStmt); ok {
			if !isTail || i != len(list)-1 {
				return "", em.errorf(ret, "early return is unsupported; put it in an if branch (0.3.2)")
			}
			if len(ret.Results) != 1 {
				return "", em.errorf(ret, "return must yield exactly one value (multi-value return is 0.3.3+)")
			}
			e, err := em.emitExpr(ret.Results[0])
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			continue
		}
		e, err := em.emitStmt(s)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ",\n"), nil
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'If|Branch' -v`  (catches all if/else/bare-if/branch-scoping/rejection tests)
Expected: PASS all seven.

- [ ] **Step 7: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS — existing tests (including the receive path, which routes through `emitStmts` with `isTail` on the clause body) still green.

- [ ] **Step 8: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): if/else and bare-if base case -> Erlang case, with branch scoping"
```

---

### Task 3: Real-toolchain rung — a runnable recursive function

Prove the base-case loop actually closes: a factorial fixture transpiles, compiles with `erlc`, AND runs with a checked result (`fact(5) = 120`). Per `run-real-toolchain-build-early`, and going beyond compile-only (the 0.3.1 rung) because control flow's correctness is a runtime property.

**Files:**
- Create: `testdata/controlflow/fact.go`
- Modify: `internal/pkg/ladder/ladder_integration_test.go` (add the rung; reuses `transpileToErl` and the `erlang.Layout` pattern already in that file)

**Interfaces:**
- Consumes (already in `ladder_integration_test.go`): `transpileToErl(t, goPath, dir) string`; `home, _ := os.UserHomeDir()`; `l := erlang.NewLayout(home, erlang.DefaultVersion)`; skip on `!l.Installed()`; `l.Erlc()`, `l.Erl()`; `strings` is already imported.
- Produces: green rung `TestRung_ControlFlowRecursion` under `-tags integration ./internal/pkg/ladder/`.

- [ ] **Step 1: Write the fixture**

Create `testdata/controlflow/fact.go`:

```go
// Package fact is a 0.3.2 control-flow fixture: a runnable recursive factorial
// exercising operators (=:=, *, -) and a bare-if base case. It transpiles,
// compiles with erlc, and runs to a checked result.
package fact

// Fact returns N! via a base-case recursion.
func Fact(N int) int {
	if N == 0 {
		return 1
	}
	return N * Fact(N-1)
}
```

- [ ] **Step 2: Write the failing rung**

Append to `internal/pkg/ladder/ladder_integration_test.go`:

```go
// TestRung_ControlFlowRecursion transpiles the 0.3.2 factorial fixture
// (operators + bare-if base case), compiles it with erlc, and RUNS it — a
// runtime check that the recursion terminates, not just that it compiles.
func TestRung_ControlFlowRecursion(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	dir := t.TempDir()
	erl := transpileToErl(t, filepath.FromSlash("../../../testdata/controlflow/fact.go"), dir)
	if out, err := exec.Command(l.Erlc(), "-o", dir, erl).CombinedOutput(); err != nil {
		t.Fatalf("erlc %s: %v\n%s", erl, err, out)
	}
	out, err := exec.Command(l.Erl(), "-noshell", "-pa", dir,
		"-eval", "io:format(\"~p\", [fact:fact(5)]), init:stop().").CombinedOutput()
	if err != nil {
		t.Fatalf("erl run: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "120" {
		t.Fatalf("fact(5) = %q, want 120", got)
	}
}
```

- [ ] **Step 3: Run the rung — must compile AND run to 120**

If `erlc` is not installed, provision first: `./bin/wm erlang install`.

Run: `go test -tags integration ./internal/pkg/ladder/ -run ControlFlowRecursion -v`
Expected: PASS — output is `120`. A **syntax error** from `erlc`, or any output other than `120`, is a real defect in the emitted Erlang (the `if`/operator emission) — fix the emitter (add a failing unit test reproducing it), not the rung.

- [ ] **Step 4: Commit**

```bash
git add testdata/controlflow/fact.go internal/pkg/ladder/ladder_integration_test.go
git commit -s -m "test(transpile): real-toolchain rung — recursive factorial compiles and runs"
```

---

### Task 4: Verification gate + docs refresh

Run the full gate, confirm the SDK-index is unchanged (no `pkg/` change), update `HANDOVER.md`. No new production code.

**Files:**
- Modify: `HANDOVER.md`
- Verify only: `docs/SDK-INDEX.md` (expected unchanged)

- [ ] **Step 1: Build + unit gate**

Run:
```bash
go build -o bin/wm ./cmd/wm
go test ./...
```
Expected: build clean, all packages green.

- [ ] **Step 2: Integration gate**

Run:
```bash
go test -tags integration ./internal/pkg/ladder/
go test -tags integration ./internal/pkg/cli/
```
Expected: all rungs green (incl. the new recursion rung). If leftover BEAM nodes cause an odd failure, clear with `pkill -9 -x beam.smp; pkill -9 -x epmd` and re-run (see `integration-test-leftover-nodes`).

- [ ] **Step 3: Security sweep (baseline check)**

Run:
```bash
govulncheck ./...
gosec ./...
gitleaks detect
```
Expected: `govulncheck`/`gitleaks` clean; `gosec` unchanged in category from the 0.3.1 baseline (accepted G703 class only; ZERO findings in the transpile package — it stays pure string emission). A NEW unaccepted HIGH/CRITICAL must be triaged.

- [ ] **Step 4: Update the handover**

Update `HANDOVER.md`: 0.3.2 delivered (operators + `if` → `case`, branch scoping, runnable-recursion rung), the verification-gate results with the run date, and set the next step to 0.3.3 (`switch` → `case`-on-value; then gen_server callbacks / `gen_statem`). Note in the delivered section that branch scoping was built per the `bound-set-integration` invariant. Move the deferred items (else-if chains, side-effect-only if, switch) into the backlog list.

- [ ] **Step 5: Commit**

```bash
git add HANDOVER.md docs/SDK-INDEX.md
git commit -s -m "docs: handover — 0.3.2 control flow complete, gate green"
```

---

## Notes for the implementer

- **Release/merge is out of this plan's scope.** These tasks land on a working branch on `origin`. Promotion to `main`, `VERSION` bump to `0.3.2`, tagging `v0.3.2`, the Copilot gate, pushing to the gated remotes, and the GitHub release are the finishing flow — done after the plan is verified green.
- **Branch scoping is the load-bearing correctness point.** The `emitBranch` snapshot/restore is exactly the `bound-set-integration` invariant that the 0.3.1 Copilot gate caught the transpiler violating for receive patterns. Do not remove or weaken the snapshot; the sibling-reuse and outer-collision tests guard it.
- **An `if` consumes the rest of its statement list** (everything after it is the continuation / false branch), so `emitStmts` appends the case and returns immediately. Statements syntactically after a full if/else are unreachable and rejected.
- **Parenthesize binary operands, don't build a precedence table.** `emitOperand` wraps a binary operand in parens; this is always correct and keeps single-operator output (`X + Y`) unchanged.
- **`maps.Clone(nil)` returns nil**, but `em.bound` is always seeded per plain function before `emitStmts` runs, so branches never hit a nil map; no defensive guard needed.
