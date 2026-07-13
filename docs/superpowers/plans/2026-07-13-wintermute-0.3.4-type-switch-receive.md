# Wintermute 0.3.4 — Type-switch receive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lower `switch v := otp.Receive().(type)` with struct-typed cases to a multi-clause Erlang `receive … end`.

**Architecture:** A dedicated emit path. `emitStmts` recognizes a tail-position `*ast.TypeSwitchStmt` whose operand is `otp.Receive()` and calls a new `emitTypeSwitchReceive`, which emits one `receive` clause per `case`, reusing an extracted `structPattern` helper (shared with the 0.3.1 single-clause receive) to build each `{tag, Field…}` pattern and bind its fields. `default:` becomes a trailing catch-all `_`; its absence yields a selective receive.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`, `strconv`, `strings`, `maps`). Erlang/OTP 29 via `erlc` for the integration rung.

## Global Constraints

- Module path: `go.muehmer.eu/wintermute` (not the GitHub path).
- Stdlib only — no third-party modules.
- TDD, red→green: write the failing test, watch it fail, then implement.
- `main()` → `run()` pattern; all logic in `internal/pkg/`, fully TDD-covered.
- Erlang variables are uppercase-leading; a lowercase ident would emit an atom (silently wrong) and must be rejected, never auto-capitalized.
- Every new binding context snapshots/restores `em.bound` and rejects collisions with outer bindings (the `bound-set-integration` invariant).
- Build binaries to `bin/` only: `go build -o bin/wm ./cmd/wm`.
- `testdata/` Go fixtures are read as source by the transpiler / integration tests; they are not built by `go test ./...`.

---

## File structure

- **Modify** `internal/pkg/transpile/transpile.go`:
  - extract `structPattern` from `receiveHead` (Task 1);
  - add `tsAlias string` field to `emitter` + alias guard in `emitExpr` (Task 4);
  - add `emitTypeSwitchReceive` (Tasks 2, 3, 4);
  - add a `*ast.TypeSwitchStmt` dispatch block in `emitStmts` (Task 2);
  - refine the `*ast.TypeSwitchStmt` reject in `emitStmt` (Task 4);
  - add a `*ast.TypeSwitchStmt` case to `terminates()` (Task 5).
- **Modify** `internal/pkg/transpile/transpile_test.go`: unit tests (Tasks 1–5).
- **Create** `testdata/typeswitch/dispatch.go`: integration fixture (Task 6).
- **Modify** `internal/pkg/ladder/ladder_integration_test.go`: add `TestRung_TypeSwitchReceive` (Task 6).

## AST reference (for all tasks)

For `switch v := otp.Receive().(type) { case Ping: … }`:
- node is `*ast.TypeSwitchStmt`.
- `ts.Assign` is an `*ast.AssignStmt` (`v := otp.Receive().(type)`); its `Lhs[0].(*ast.Ident).Name` is the alias `v`. (For the tagless `switch otp.Receive().(type)` form, `ts.Assign` is an `*ast.ExprStmt` — deferred, reject.)
- `ts.Assign.(*ast.AssignStmt).Rhs[0]` is an `*ast.TypeAssertExpr` with `.Type == nil` (the `.(type)` marker) and `.X` the operand.
- The operand `.X` must be a `*ast.CallExpr` c with `isOtpCall(c, "Receive")` true.
- `ts.Body.List` holds `*ast.CaseClause`s. `cc.List == nil` is `default`. Otherwise `cc.List` holds the case type expressions: `*ast.Ident` (`Ping`) or `*ast.StarExpr` whose `.X` is `*ast.Ident` (`*Ping`). `len(cc.List) > 1` is a multi-type case.

---

### Task 1: Extract `structPattern` helper

Pull the tuple-pattern construction out of `receiveHead` into a reusable helper that also rejects an unknown struct type, so both the single-clause receive and the new type switch build patterns identically.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`receiveHead`, ~lines 558-584)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `func (em *emitter) structPattern(typeName string, at ast.Node) (string, error)` — returns the Erlang tuple pattern `{tag, F1, F2, …}` (tag = `strings.ToLower(typeName)`, one part per declared field), registers each field in `em.bound`, and returns an error if `typeName` is not a declared struct or a field collides with an existing binding.

- [x] **Step 1: Write the failing test**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestModule_ReceiveUnknownStructTypeRejected(t *testing.T) {
	// otp.Receive().(T) where T is not a declared struct must be rejected,
	// not silently emitted as a fieldless {t} pattern.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F() {
	M := otp.Receive().(Ghost)
	otp.Print(M.Whatever)
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "unknown struct type") {
		t.Fatalf("want unknown-struct-type rejection, got %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_ReceiveUnknownStructTypeRejected -v`
Expected: FAIL (today an unknown type yields a fieldless pattern, so no error — the test's `err == nil` branch fires).

- [x] **Step 3: Extract the helper and call it from `receiveHead`**

Add this helper (place it just above `receiveHead`):

```go
// structPattern returns the Erlang tuple pattern for a declared struct type
// (atom = lowercased type name, each declared field bound to its capitalized
// field name) and registers each field name as a bound Erlang variable. It
// errors if typeName is not a declared struct, or if a field name collides with
// an already-bound name — in Erlang an already-bound pattern variable is an
// equality match, not a fresh binding, so emitting it would silently change the
// semantics.
func (em *emitter) structPattern(typeName string, at ast.Node) (string, error) {
	fields, ok := em.structs[typeName]
	if !ok {
		return "", em.errorf(at, "unknown struct type %s", typeName)
	}
	parts := []string{strings.ToLower(typeName)}
	for _, fld := range fields {
		if em.bound[fld] {
			return "", em.errorf(at, "receive pattern field %s collides with an already-bound name; Erlang would treat it as an equality match, not a fresh binding — rename one", fld)
		}
		em.bound[fld] = true
		parts = append(parts, fld)
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}
```

Replace the tail of `receiveHead` (the loop from `parts := []string{strings.ToLower(typ.Name)}` through the `return "{" + … + "}", list[1:], nil`) with:

```go
	pat, err := em.structPattern(typ.Name, as)
	if err != nil {
		return "", nil, err
	}
	return pat, list[1:], nil
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'TestModule_Receive' -v`
Expected: PASS (the new reject test and all existing receive tests are green — the refactor preserves behavior for declared types).

- [x] **Step 5: Run the full package to confirm no regression**

Run: `go test ./internal/pkg/transpile/`
Expected: `ok`.

- [x] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "refactor(transpile): extract structPattern helper, reject unknown receive struct type"
```

---

### Task 2: Recognize and lower the happy path (receive with default)

Recognize a tail-position `switch v := otp.Receive().(type)` and emit a multi-clause `receive … end` with a trailing catch-all for `default:`.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmts` ~line 342; new `emitTypeSwitchReceive`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `structPattern` (Task 1).
- Produces: `func (em *emitter) emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error)` — returns a full `receive … end` string.
- Produces: a `*ast.TypeSwitchStmt` dispatch in `emitStmts` guarded by `isReceiveTypeSwitch(ts)`.
- Produces: `func isReceiveTypeSwitch(ts *ast.TypeSwitchStmt) bool`.

- [x] **Step 1: Write the failing test**

```go
func TestModule_TypeSwitchReceiveWithDefault(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data }
type Pong struct{ To }
func Handle() {
	switch v := otp.Receive().(type) {
	case Ping:
		otp.Print(v.Data)
	case *Pong:
		otp.Print(v.To)
	default:
		otp.Print(Data)
	}
}`
	out, err := Module(src)
	if err != nil {
		t.Fatalf("want type-switch receive accepted, got %v", err)
	}
	// `case Ping` and `case *Pong` must both lower to the same tuple tag —
	// Erlang has no pointers, so the star is meaningless.
	for _, want := range []string{"receive", "{ping, Data} ->", "{pong, To} ->", "_ ->", "end"} {
		if !strings.Contains(out.Erl, want) {
			t.Fatalf("missing %q in:\n%s", want, out.Erl)
		}
	}
}
```

Note: the fixture's structs use single fields (`Data`, `To`) whose names are the bound variables; `otp.Print(v.Data)` lowers via the existing `SelectorExpr` path to `Print(Data)`. The `case *Pong` clause covers the pointer form (spec: `case *Pong` equals `case Pong`). The `default:` body uses `otp.Print(Data)` — a bare field variable, not the alias — so this task's test stays green independent of Task 4's bare-alias guard.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchReceiveWithDefault -v`
Expected: FAIL with "type switch is unsupported (0.3.4+)" (the current `emitStmt` reject).

- [x] **Step 3: Add recognition + emit**

Add the recognizer (near `isReceiveTypeSwitch`'s siblings, e.g. after `otpPkgIdent`):

```go
// isReceiveTypeSwitch reports whether ts is `v := otp.Receive().(type)` — a
// type switch whose operand is otp.Receive(). This is the only type-switch form
// 0.3.4 supports (lowered to a multi-clause receive); every other form errors.
func isReceiveTypeSwitch(ts *ast.TypeSwitchStmt) bool {
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return false
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok || ta.Type != nil { // .(type), not .(T)
		return false
	}
	call, ok := ta.X.(*ast.CallExpr)
	return ok && isOtpCall(call, "Receive")
}
```

Add the dispatch in `emitStmts`, immediately after the `*ast.SwitchStmt` block (after line ~342, before the `*ast.ReturnStmt` block):

```go
		if ts, ok := s.(*ast.TypeSwitchStmt); ok {
			if !isTail {
				return "", em.errorf(ts, "control flow (type switch) is only supported in tail position")
			}
			if i != len(list)-1 {
				return "", em.errorf(list[i+1], "unreachable statement after a type switch")
			}
			e, err := em.emitTypeSwitchReceive(ts)
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			return strings.Join(parts, ",\n"), nil
		}
```

Add `emitTypeSwitchReceive` (place it after `emitSwitch`):

```go
// emitTypeSwitchReceive lowers `switch v := otp.Receive().(type)` to a
// multi-clause Erlang `receive {tag, Field…} -> body; … end`. Each case names a
// single declared struct type; its fields are bound in a per-clause scope
// (em.bound snapshotted and restored) via structPattern, exactly like the
// single-clause receive. A `default:` becomes a trailing catch-all `_`; without
// it the receive is selective (non-matching messages stay in the mailbox). Only
// the otp.Receive() operand is supported here (isReceiveTypeSwitch gates entry).
func (em *emitter) emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error) {
	if !isReceiveTypeSwitch(ts) {
		return "", em.errorf(ts, "type switch on a plain value is unsupported (0.3.5+); the operand must be otp.Receive()")
	}
	var clauses []string
	var deflt string
	haveDefault := false
	for _, s := range ts.Body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			return "", em.errorf(s, "unsupported type-switch clause")
		}
		if len(cc.Body) == 0 {
			return "", em.errorf(cc, "case clause has no value (empty body)")
		}
		if cc.List == nil { // default
			if haveDefault {
				return "", em.errorf(cc, "type switch has more than one default")
			}
			haveDefault = true
			body, err := em.emitBranch(cc.Body)
			if err != nil {
				return "", err
			}
			deflt = body
			continue
		}
		if len(cc.List) != 1 {
			return "", em.errorf(cc, "multi-type case is unsupported (0.3.5+)")
		}
		name, err := caseTypeName(cc.List[0])
		if err != nil {
			return "", em.errorf(cc.List[0], "%v", err)
		}
		snap := maps.Clone(em.bound)
		pat, err := em.structPattern(name, cc)
		if err != nil {
			em.bound = snap
			return "", err
		}
		body, err := em.emitStmts(cc.Body, true)
		em.bound = snap
		if err != nil {
			return "", err
		}
		clauses = append(clauses, pat+" -> "+body)
	}
	if haveDefault {
		clauses = append(clauses, "_ -> "+deflt)
	}
	var b strings.Builder
	b.WriteString("receive\n")
	for i, c := range clauses {
		b.WriteString(indent(c))
		if i < len(clauses)-1 {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}
	b.WriteString("end")
	return b.String(), nil
}

// caseTypeName returns the struct type name of a type-switch case expression,
// accepting both `Ping` and `*Ping` (Erlang has no pointers, so the star is
// meaningless). Any other expression is an error.
func caseTypeName(e ast.Expr) (string, error) {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("type switch case must name a struct type")
	}
	return id.Name, nil
}
```

Also change the `emitStmt` `*ast.TypeSwitchStmt` reject (line ~628-629) so a non-tail type switch gets a clear message (a tail one never reaches `emitStmt` — `emitStmts` handles it):

```go
	case *ast.TypeSwitchStmt:
		return "", em.errorf(st, "control flow (type switch) is only supported in tail position")
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchReceiveWithDefault -v`
Expected: PASS.

- [x] **Step 5: Run the full package**

Run: `go test ./internal/pkg/transpile/`
Expected: `ok`.

- [x] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): lower switch v := otp.Receive().(type) to multi-clause receive"
```

---

### Task 3: Selective receive (no default)

A type switch with no `default:` must emit a selective receive — the listed clauses only, no catch-all.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (already handled by Task 2's `haveDefault` guard — this task adds the test that locks the behavior)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitTypeSwitchReceive` (Task 2).

- [x] **Step 1: Write the failing test**

```go
func TestModule_TypeSwitchReceiveSelectiveNoDefault(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data }
type Pong struct{ To }
func Handle() {
	switch v := otp.Receive().(type) {
	case Ping:
		otp.Print(v.Data)
	case Pong:
		otp.Print(v.To)
	}
}`
	out, err := Module(src)
	if err != nil {
		t.Fatalf("want selective receive accepted, got %v", err)
	}
	if !strings.Contains(out.Erl, "{ping, Data} ->") || !strings.Contains(out.Erl, "{pong, To} ->") {
		t.Fatalf("missing struct clauses in:\n%s", out.Erl)
	}
	if strings.Contains(out.Erl, "_ ->") {
		t.Fatalf("selective receive must have no catch-all, got:\n%s", out.Erl)
	}
}
```

- [x] **Step 2: Run test to verify it passes (behavior already correct)**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchReceiveSelectiveNoDefault -v`
Expected: PASS if Task 2's `haveDefault` guard is correct (no default → no `_ ->` clause). This task is a lock-in test; if it fails, fix `emitTypeSwitchReceive` so the catch-all is appended only when `haveDefault`.

- [x] **Step 3: (No implementation needed if green.)**

If the test failed, ensure the `if haveDefault { clauses = append(clauses, "_ -> "+deflt) }` guard in `emitTypeSwitchReceive` is present and correct, then re-run.

- [x] **Step 4: Run the full package**

Run: `go test ./internal/pkg/transpile/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile_test.go
git commit -s -m "test(transpile): lock selective (no-default) type-switch receive"
```

---

### Task 4: Rejects and the bare-alias guard

Add the remaining reject rules and the guard that forbids using the alias `v` bare (only `v.Field` is allowed).

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitter` struct ~line 22; `emitExpr` `*ast.Ident` case ~line 750; `emitTypeSwitchReceive`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitTypeSwitchReceive` (Task 2).
- Produces: `emitter.tsAlias string` — the active type-switch alias name during clause-body emission; empty otherwise.

- [x] **Step 1: Write the failing tests**

```go
func TestModule_TypeSwitchRejects(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			name: "plain value operand",
			src: `package m
type Ping struct{ Data }
func F(X int) {
	switch v := X.(type) {
	case Ping:
		_ = v
	}
}`,
			want: "plain value",
		},
		{
			name: "non-struct case",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F() {
	switch v := otp.Receive().(type) {
	case int:
		otp.Print(v)
	}
}`,
			want: "must name a struct type",
		},
		{
			name: "multi-type case",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data }
type Pong struct{ To }
func F() {
	switch v := otp.Receive().(type) {
	case Ping, Pong:
		otp.Print(v.Data)
	}
}`,
			want: "multi-type case",
		},
		{
			name: "bare alias",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data }
func F() {
	switch v := otp.Receive().(type) {
	case Ping:
		otp.Send(v.Data, v)
	}
}`,
			want: "must be used via field access",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Module(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q rejection, got %v", tc.want, err)
			}
		})
	}
}
```

Note: `otp.Send` takes two args (`otp.Send(Pid, Msg)`); the "bare alias" case passes `v` as the message. If `otp.Send`'s arity differs, use any two-arg `otp` call whose second arg is bare `v`.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchRejects -v`
Expected: the `plain value`, `non-struct case`, and `multi-type case` subtests may already pass (Task 2 emits those errors); the `bare alias` subtest FAILS (no guard yet — bare `v` emits as Erlang variable `V`).

- [x] **Step 3: Add the `tsAlias` field and the alias guard**

Add the field to `emitter` (line ~22):

```go
type emitter struct {
	structs    map[string][]string
	fset       *token.FileSet
	registered []string
	bound      map[string]bool
	tsAlias    string // active type-switch alias name during clause-body emission
}
```

In `emitTypeSwitchReceive`, set the alias around clause-body emission. Capture the alias name up front and restore on return:

```go
	as := ts.Assign.(*ast.AssignStmt) // safe: isReceiveTypeSwitch verified the shape
	alias := as.Lhs[0].(*ast.Ident).Name
	old := em.tsAlias
	em.tsAlias = alias
	defer func() { em.tsAlias = old }()
```

Insert those lines at the top of `emitTypeSwitchReceive`, immediately after the `isReceiveTypeSwitch` guard.

Add the guard in `emitExpr`'s `*ast.Ident` case (line ~750), before the lowercase check:

```go
	case *ast.Ident:
		if em.tsAlias != "" && ex.Name == em.tsAlias {
			return "", em.errorf(ex, "the type-switch alias %s must be used via field access (%s.Field); passing the whole value is unsupported (0.3.5+)", ex.Name, ex.Name)
		}
		if !token.IsExported(ex.Name) {
			return "", em.errorf(ex, "bare identifier %s is lowercase-leading; Erlang variables must be uppercase", ex.Name)
		}
		return ex.Name, nil
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchRejects -v`
Expected: all subtests PASS.

- [x] **Step 5: Run the full package**

Run: `go test ./internal/pkg/transpile/`
Expected: `ok`.

- [x] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): reject non-struct/multi-type/plain-value type switch and bare alias use"
```

---

### Task 5: `terminates()` — a type-switch receive counts as terminating

A type-switch receive terminates once every clause terminates; a `default` is **not** required (a `receive` cannot fall through). So it may be the then-branch of a bare `if`.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`terminates`, ~lines 423-436)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitTypeSwitchReceive` (Task 2).

- [x] **Step 1: Write the failing test**

```go
func TestModule_BareIfTypeSwitchReceiveThenAccepted(t *testing.T) {
	// A bare-if whose then-branch is a type-switch receive yields a value and
	// does not fall through (a receive proceeds only on a match), so it may be
	// the then-branch, exactly like an if/else — no default required.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data }
func F(N int) string {
	if N > 0 {
		switch v := otp.Receive().(type) {
		case Ping:
			return v.Data
		}
	}
	return "skip"
}`
	out, err := Module(src)
	if err != nil {
		t.Fatalf("want bare-if-with-type-switch-receive accepted, got %v", err)
	}
	if !strings.Contains(out.Erl, "case N > 0 of") || !strings.Contains(out.Erl, "receive") {
		t.Fatalf("want both the bare-if case and the receive, got:\n%s", out.Erl)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_BareIfTypeSwitchReceiveThenAccepted -v`
Expected: FAIL with "the then-branch of a bare if must end in a return" (`terminates()` does not yet know `*ast.TypeSwitchStmt`).

- [x] **Step 3: Add the case to `terminates()`**

In `terminates()` (line ~427), add a case after the `*ast.SwitchStmt` case:

```go
	case *ast.TypeSwitchStmt:
		// A type-switch receive terminates once every clause terminates; unlike
		// a case-switch it needs NO default, because a receive cannot fall
		// through — it proceeds only on a match and yields that clause's value.
		for _, cc := range s.Body.List {
			clause, ok := cc.(*ast.CaseClause)
			if !ok || !terminates(clause.Body) {
				return false
			}
		}
		return true
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_BareIfTypeSwitchReceiveThenAccepted -v`
Expected: PASS.

- [x] **Step 5: Run the full package**

Run: `go test ./internal/pkg/transpile/`
Expected: `ok`.

- [x] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): treat a type-switch receive as terminating (no default needed)"
```

---

### Task 6: Runnable rung — real-toolchain integration

Prove the multi-clause receive dispatch works end-to-end: a fixture transpiles, compiles with `erlc`, and RUNS, dispatching two message types to the right clause.

**Files:**
- Create: `testdata/typeswitch/dispatch.go`
- Modify: `internal/pkg/ladder/ladder_integration_test.go` (add `TestRung_TypeSwitchReceive`)

**Interfaces:**
- Consumes: `transpileToErl`, `erlc` helpers already in `ladder_integration_test.go` (see `TestRung_Switch`, ~line 143).

- [x] **Step 1: Write the fixture**

Create `testdata/typeswitch/dispatch.go`:

```go
// Package dispatch is a 0.3.4 type-switch-receive fixture: a function receives a
// tagged message and dispatches on its type, returning a word per type. It
// transpiles, compiles with erlc, and runs to a checked result.
package dispatch

import "go.muehmer.eu/wintermute/pkg/otp"

type Ping struct{ Data }
type Pong struct{ Data }

// Handle receives one message and returns a word identifying its type. With the
// message already in the mailbox, the selective receive matches immediately.
func Handle() string {
	switch v := otp.Receive().(type) {
	case Ping:
		return v.Data
	case Pong:
		return v.Data
	default:
		return "other"
	}
}
```

Note: both structs carry a `Data` field so each clause returns its payload. Expected Erlang: `handle() -> receive {ping, Data} -> Data; {pong, Data} -> Data; _ -> <<"other">> end.`

- [x] **Step 2: Write the failing integration test**

Add to `internal/pkg/ladder/ladder_integration_test.go`:

```go
// TestRung_TypeSwitchReceive transpiles a 0.3.4 type-switch receive, compiles it
// with erlc, and RUNS it — proving the multi-clause receive dispatch closes: a
// {ping, …} message hits the ping clause, a {pong, …} the pong clause.
func TestRung_TypeSwitchReceive(t *testing.T) {
	requireErlang(t)
	dir := t.TempDir()
	erl := transpileToErl(t, filepath.FromSlash("../../../testdata/typeswitch/dispatch.go"), dir)
	if out, err := exec.Command(erlcPath(t), "-o", dir, erl).CombinedOutput(); err != nil {
		t.Fatalf("erlc %s: %v\n%s", erl, err, out)
	}
	for _, tc := range []struct{ send, want string }{
		{`{ping, <<"P">>}`, "P"},
		{`{pong, <<"Q">>}`, "Q"},
	} {
		eval := "self() ! " + tc.send + ", io:format(\"~s\", [dispatch:handle()]), init:stop()."
		out, err := exec.Command(erlPath(t), "-noshell", "-pa", dir, "-eval", eval).CombinedOutput()
		if err != nil {
			t.Fatalf("run %s: %v\n%s", tc.send, err, out)
		}
		if got := string(out); got != tc.want {
			t.Fatalf("send %s: got %q, want %q", tc.send, got, tc.want)
		}
	}
}
```

Note: match the exact helper names/signatures used by `TestRung_Switch` in this file — `requireErlang`, `transpileToErl`, `erlcPath`, `erlPath` may be named differently (e.g. an inline `erlcPath(t)`/`erlPath(t)` or a package-level const). Read `TestRung_Switch` (~line 143) and mirror its helpers exactly. The `-eval` seeds the mailbox with `self() ! Msg` so the selective receive matches immediately.

- [x] **Step 3: Run the test to verify it fails first for the right reason**

Run: `go test -tags integration -count=1 ./internal/pkg/ladder/ -run TestRung_TypeSwitchReceive -v`
Expected: FAIL — initially because the fixture path or helper names need aligning, or (if Tasks 2/4 are incomplete) a transpile error. Once wired, it must reach the run step.

- [x] **Step 4: Align helpers and re-run to green**

Fix the helper names to match the file, then run:

Run: `go test -tags integration -count=1 ./internal/pkg/ladder/ -run TestRung_TypeSwitchReceive -v`
Expected: PASS — both `{ping, …}` → `P` and `{pong, …}` → `Q`.

Clear leftover BEAM nodes first if the suite flakes: `pkill -9 -x beam.smp; pkill -9 -x epmd`.

- [x] **Step 5: Run both integration suites and the unit suite**

Run:
```bash
go test ./...
go test -tags integration -count=1 ./internal/pkg/ladder/ ./internal/pkg/cli/
```
Expected: all green. `-count=1` because `transpile.go` changed — don't trust the cache.

- [x] **Step 6: Commit**

```bash
git add testdata/typeswitch/dispatch.go internal/pkg/ladder/ladder_integration_test.go
git commit -s -m "test(transpile): real-toolchain rung — type-switch receive dispatches two message types"
```

---

## Self-review

**Spec coverage:**
- Recognize `switch v := otp.Receive().(type)` → Task 2. ✓
- Multi-clause `receive` lowering → Task 2. ✓
- Per-clause tuple pattern + field binding (reuse 0.3.1 builder) → Task 1 (extract) + Task 2 (use). ✓
- `v.Field` → `Field` in body → existing `SelectorExpr` path, exercised by Task 2/3 tests. ✓
- Optional default (with → catch-all, without → selective) → Task 2 (with) + Task 3 (without). ✓
- Per-clause `em.bound` snapshot/restore + outer-collision reject → Task 2 (snapshot/restore) + Task 1 (`structPattern` collision check). ✓
- `case *Ping` = `case Ping` → Task 2 (`caseTypeName` unwraps `StarExpr`; the happy-path fixture uses `case *Pong` and asserts `{pong, To} ->`). ✓
- Rejects: plain-value operand, non-struct case, multi-type case, empty body, bare alias, unknown struct, not-tail → Task 2 (most) + Task 4 (bare alias) + Task 1 (unknown struct) + Task 2 (`emitStmt`/`emitStmts` tail guards). ✓
- `terminates()` integration (no default needed) → Task 5. ✓
- Runnable rung → Task 6. ✓
- Coexistence with 0.3.1 single-clause receive → Task 1 keeps `receiveHead` behavior for declared types; existing receive tests stay green (Task 1 Step 4). ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code. Task 6 flags that helper names must be read from the existing file — that is an alignment instruction with an exact reference (`TestRung_Switch`), not a placeholder.

**Type consistency:** `structPattern(typeName string, at ast.Node) (string, error)`, `emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error)`, `isReceiveTypeSwitch(ts *ast.TypeSwitchStmt) bool`, `caseTypeName(e ast.Expr) (string, error)`, and `emitter.tsAlias string` are used consistently across Tasks 1–5. `emitBranch` is reused only for the `default:` body (no pattern binding); struct clauses use explicit `maps.Clone` snapshot + `structPattern` + `emitStmts` + restore.

## Pre-merge (after all tasks)

`go test ./...` green; both integration suites `-count=1` green; `pgrep beam.smp` = 0; govulncheck/gitleaks/gosec baseline; then the gated-release ritual (VERSION bump, squash, Copilot gate on the release diff, push origin→upstream→github, GitHub release, `production-0.3.4`, refresh README + handover). See HANDOVER "Release ritual".
