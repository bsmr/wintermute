# Wintermute 0.3.5 — Plain-value type switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Lower `switch v := X.(type)` over any value operand `X` (struct-typed cases) to an Erlang `case X of … end`, generalizing the 0.3.4 receive type switch to the value operand.

**Architecture:** The 0.3.4 receive path already builds the per-clause tuple patterns, tag-collision rejection, `em.bound` scoping, and optional `default:` handling — all operand-independent. This plan extracts that clause loop into a shared `emitTypeSwitchClauses` helper (Task 1), then adds a single `emitTypeSwitch` entry point that dispatches on the operand: `otp.Receive()` → `receive … end` (existing), any other value → `case <operand> of … end` (new). The value path reuses every existing guard for free; the only genuinely new code is emitting the scrutinee via `emitExpr` and the `case … of` wrapper.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`, `strings`, `maps`). Erlang/OTP 29 via `erlc` for the integration rung.

## Global Constraints

- Module path: `go.muehmer.eu/wintermute` (not the GitHub path).
- Stdlib only — no third-party modules.
- TDD, red→green: write the failing test, watch it fail, then implement.
- `main()` → `run()` pattern; all logic in `internal/pkg/`, fully TDD-covered.
- Erlang variables are uppercase-leading. A lowercase ident emits an atom (silently wrong) and must be rejected, never auto-capitalized. This applies to the type-switch **operand** and **alias**: `switch V := M.(type)`, not `switch v := m.(type)`.
- Every new binding context snapshots/restores `em.bound` and rejects collisions with outer bindings (the `bound-set-integration` invariant). The value path inherits this via the shared clause helper — no new binding context is introduced.
- Build binaries to `bin/` only: `go build -o bin/wm ./cmd/wm`.
- `testdata/` Go fixtures are read as source by the transpiler / integration tests; they are not built by `go test ./...`.
- Struct fixture fields MUST be typed (`struct{ Data int }`), never bare `struct{ Data }` (a bare embedded field registers zero fields).

---

## File structure

- **Modify** `internal/pkg/transpile/transpile.go`:
  - extract `emitTypeSwitchClauses` from `emitTypeSwitchReceive` (Task 1);
  - add `emitTypeSwitch` entry point + `emitTypeSwitchValue`; make `emitTypeSwitchReceive` a thin wrapper (Task 2);
  - change the `*ast.TypeSwitchStmt` dispatch in `emitStmts` to call `emitTypeSwitch` (Task 2);
  - remove the `"type switch on a plain value is unsupported (0.3.5+)"` error (Task 2);
  - bump deferred-feature version strings `(0.3.5+)` → `(0.3.6+)` (Task 3);
  - update the `terminates()` `*ast.TypeSwitchStmt` comment (receive → receive/case) (Task 3).
- **Modify** `internal/pkg/transpile/transpile_test.go`: unit tests (Tasks 1–3).
- **Create** `testdata/typeswitch/classify.go`: integration fixture (Task 4).
- **Modify** `internal/pkg/ladder/ladder_integration_test.go`: add `TestRung_TypeSwitchValue` (Task 4).

## AST reference (for all tasks)

For `switch V := M.(type) { case Ping: … }`:
- node is `*ast.TypeSwitchStmt`.
- `ts.Assign` is an `*ast.AssignStmt` (`V := M.(type)`); `ts.Assign.(*ast.AssignStmt).Lhs[0].(*ast.Ident).Name` is the alias `V`. For the tagless form `switch M.(type)`, `ts.Assign` is an `*ast.ExprStmt` — reject (deferred).
- `ts.Assign.(*ast.AssignStmt).Rhs[0]` is an `*ast.TypeAssertExpr` `ta` with `ta.Type == nil` (the `.(type)` marker). `ta.X` is the operand: `otp.Receive()` (a `*ast.CallExpr`) for the receive path, or any other expression (`*ast.Ident` `M`, a call, a field access) for the value path.
- `ts.Body.List` holds `*ast.CaseClause`s. `cc.List == nil` is `default`. Otherwise `cc.List` holds the case type expressions: `*ast.Ident` (`Ping`) or `*ast.StarExpr` whose `.X` is `*ast.Ident` (`*Ping`). `len(cc.List) > 1` is a multi-type case.

## Current shape of `emitTypeSwitchReceive` (transpile.go ~574–649)

Today the function does, in order: `isReceiveTypeSwitch` gate → extract `alias` → set/restore `em.tsAlias` → reject `ts.Init` → loop over clauses (parse `*ast.CaseClause`, reject empty body, handle `default`, reject multi-type, `caseTypeName` → tag, `seenTag` collision check, `em.bound` snapshot, `structPattern`, emit body, restore) building `clauses` (+ trailing `_ -> deflt`) → wrap in `receive … end`. Tasks 1–2 split this: the clause loop becomes `emitTypeSwitchClauses`; the alias/init/dispatch/wrapper concerns move to `emitTypeSwitch`.

---

### Task 1: Extract `emitTypeSwitchClauses` (pure refactor)

Extract the per-clause loop from `emitTypeSwitchReceive` into a reusable helper. Behaviour is unchanged: the receive path calls the helper and wraps the result in `receive … end`. No new feature; the existing 0.3.4 receive test suite is the safety net and must stay green.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitTypeSwitchReceive`, ~574–649)
- Test: none new — existing receive tests in `internal/pkg/transpile/transpile_test.go` guard the refactor.

**Interfaces:**
- Produces: `func (em *emitter) emitTypeSwitchClauses(ts *ast.TypeSwitchStmt) ([]string, error)` — returns the ordered Erlang clause strings (`"<pattern> -> <body>"`), including a trailing `"_ -> <deflt>"` when a `default:` is present. Rejects: non-`*ast.CaseClause` body entries, empty clause bodies, a second `default`, multi-type cases, non-struct cases (via `caseTypeName`), and duplicate message tags. Snapshots/restores `em.bound` per clause. Assumes `em.tsAlias` is already set by the caller.

- [x] **Step 1: Confirm the receive suite is green (baseline)**

Run: `go test ./internal/pkg/transpile/ -run TypeSwitchReceive -v`
Expected: PASS (all existing 0.3.4 receive tests). This is the baseline the refactor must preserve.

- [x] **Step 2: Extract the helper**

In `transpile.go`, add the new helper (place it directly after `emitTypeSwitchReceive`). Move the clause loop body verbatim out of `emitTypeSwitchReceive`:

```go
// emitTypeSwitchClauses emits the ordered Erlang clauses for a type switch's
// case bodies: one "Pattern -> Body" per struct case, plus a trailing
// "_ -> Body" when a default: is present. Each clause names a single declared
// struct type (caseTypeName), binds its fields in a snapshotted em.bound scope
// (structPattern), and two cases lowering to the same message tag are rejected
// (the second would be unreachable in Erlang). em.tsAlias must be set by the
// caller so v.Field access resolves and a bare alias is rejected. Operand- and
// wrapper-agnostic: shared by the receive (receive … end) and value (case X of
// … end) paths.
func (em *emitter) emitTypeSwitchClauses(ts *ast.TypeSwitchStmt) ([]string, error) {
	var clauses []string
	var deflt string
	haveDefault := false
	seenTag := map[string]bool{}
	for _, s := range ts.Body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			return nil, em.errorf(s, "unsupported type-switch clause")
		}
		if len(cc.Body) == 0 {
			return nil, em.errorf(cc, "case clause has no value (empty body)")
		}
		if cc.List == nil { // default
			if haveDefault {
				return nil, em.errorf(cc, "type switch has more than one default")
			}
			haveDefault = true
			body, err := em.emitBranch(cc.Body)
			if err != nil {
				return nil, err
			}
			deflt = body
			continue
		}
		if len(cc.List) != 1 {
			return nil, em.errorf(cc, "multi-type case is unsupported (0.3.5+)")
		}
		name, err := em.caseTypeName(cc.List[0])
		if err != nil {
			return nil, err
		}
		tag := strings.ToLower(name)
		if seenTag[tag] {
			return nil, em.errorf(cc.List[0], "type switch has two cases with the same message tag %q; the second clause would be unreachable in Erlang (e.g. Ping and *Ping, or names differing only in case)", tag)
		}
		seenTag[tag] = true
		snap := maps.Clone(em.bound)
		pat, err := em.structPattern(name, cc)
		if err != nil {
			em.bound = snap
			return nil, err
		}
		body, err := em.emitStmts(cc.Body, true)
		em.bound = snap
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, pat+" -> "+body)
	}
	if haveDefault {
		clauses = append(clauses, "_ -> "+deflt)
	}
	return clauses, nil
}
```

(The `(0.3.5+)` string in the multi-type error is bumped to `(0.3.6+)` in Task 3 — leave it as-is here to keep this a behaviour-preserving refactor.)

- [x] **Step 3: Reduce `emitTypeSwitchReceive` to call the helper**

Replace the body of `emitTypeSwitchReceive` so it keeps its current pre-work (gate, alias, `tsAlias`, init reject) but delegates the loop:

```go
func (em *emitter) emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error) {
	if !isReceiveTypeSwitch(ts) {
		return "", em.errorf(ts, "type switch on a plain value is unsupported (0.3.5+); the operand must be otp.Receive()")
	}
	as := ts.Assign.(*ast.AssignStmt) // safe: isReceiveTypeSwitch verified the shape
	alias := as.Lhs[0].(*ast.Ident).Name
	old := em.tsAlias
	em.tsAlias = alias
	defer func() { em.tsAlias = old }()
	if ts.Init != nil {
		return "", em.errorf(ts, "type switch with an init statement is unsupported (0.3.5+)")
	}
	clauses, err := em.emitTypeSwitchClauses(ts)
	if err != nil {
		return "", err
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
```

- [x] **Step 4: Run the full suite to verify the refactor is green**

Run: `go test ./internal/pkg/transpile/ -count=1`
Expected: PASS (identical behaviour; the receive tests still pass).

- [x] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go
git commit -m "refactor: extract emitTypeSwitchClauses from emitTypeSwitchReceive"
```

---

### Task 2: Entry-point dispatch + value path

Add the value lowering. A new `emitTypeSwitch` entry point owns the alias/`tsAlias`/init concerns and dispatches on the operand; `emitTypeSwitchReceive` becomes a thin `receive` wrapper and a new `emitTypeSwitchValue` is the `case X of` wrapper. `emitStmts` calls the entry point. The old plain-value error is removed. With and without `default:` both work (shared helper).

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmts` ~344–357; `emitTypeSwitchReceive`; add `emitTypeSwitch`, `emitTypeSwitchValue`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitTypeSwitchClauses` (Task 1); `isReceiveTypeSwitch`, `emitExpr`, `indent`.
- Produces:
  - `func (em *emitter) emitTypeSwitch(ts *ast.TypeSwitchStmt) (string, error)` — the single entry point. Rejects the tagless form and an init statement, sets `em.tsAlias`, dispatches: `isReceiveTypeSwitch` → receive wrapper, else → value wrapper.
  - `func (em *emitter) emitTypeSwitchValue(ts *ast.TypeSwitchStmt) (string, error)` — emits `case <operand> of <clauses> end`; the operand is `ts.Assign.(*ast.AssignStmt).Rhs[0].(*ast.TypeAssertExpr).X` via `emitExpr`.

- [x] **Step 1: Write the failing tests**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestModule_TypeSwitchValueWithDefault(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping:
		otp.Print(V.Data)
	case Pong:
		otp.Print(V.Data)
	default:
		otp.Print(0)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"case M of",
		"{ping, Data} -> io:format",
		"{pong, Data} -> io:format",
		"_ -> io:format",
		"end",
	} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("output missing %q\n%s", want, r.Erl)
		}
	}
}

func TestModule_TypeSwitchValueNoDefault(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping:
		otp.Print(V.Data)
	case Pong:
		otp.Print(V.Data)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "case M of") {
		t.Errorf("want case-of wrapper, got:\n%s", r.Erl)
	}
	if strings.Contains(r.Erl, "_ ->") {
		t.Errorf("no-default value switch must not emit a catch-all clause:\n%s", r.Erl)
	}
}

func TestModule_TypeSwitchTaglessRejected(t *testing.T) {
	// switch M.(type) — no alias binding — is deferred; must be rejected, not
	// silently accepted with no alias.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch M.(type) {
	case Ping:
		otp.Print(1)
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "must bind an alias") {
		t.Fatalf("want tagless rejection, got %v", err)
	}
}
```

> Confirm the exact `Result` field name for the Erlang output (`r.Erl` above) against the existing tests in `transpile_test.go` and match it; adjust if the field is named differently.

- [x] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TypeSwitchValue|TypeSwitchTagless' -v`
Expected: FAIL — `TestModule_TypeSwitchValueWithDefault`/`NoDefault` hit `"type switch on a plain value is unsupported (0.3.5+)"`; `TaglessRejected` gets the wrong error text.

- [x] **Step 3: Add the entry point and value wrapper; thin the receive wrapper**

In `transpile.go`, add `emitTypeSwitch` and `emitTypeSwitchValue`, and remove the alias/`tsAlias`/init pre-work + the `isReceiveTypeSwitch` gate from `emitTypeSwitchReceive` (they move to the entry point):

```go
// emitTypeSwitch dispatches a tail-position type switch on its operand. The
// alias-binding form `switch V := X.(type)` is required (the tagless form and
// an init statement are rejected). em.tsAlias is set for the whole emission so
// V.Field resolves in clause bodies and a bare V is rejected. otp.Receive() as
// the operand lowers to a multi-clause `receive`; any other value lowers to a
// `case X of … end`.
func (em *emitter) emitTypeSwitch(ts *ast.TypeSwitchStmt) (string, error) {
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", em.errorf(ts, "type switch must bind an alias (switch V := X.(type)); the tagless form is unsupported (0.3.6+)")
	}
	alias := as.Lhs[0].(*ast.Ident).Name
	old := em.tsAlias
	em.tsAlias = alias
	defer func() { em.tsAlias = old }()
	if ts.Init != nil {
		return "", em.errorf(ts, "type switch with an init statement is unsupported (0.3.6+)")
	}
	if isReceiveTypeSwitch(ts) {
		return em.emitTypeSwitchReceive(ts)
	}
	return em.emitTypeSwitchValue(ts)
}

// emitTypeSwitchValue lowers `switch V := X.(type)` over a plain value X to an
// Erlang `case X of {tag, Field…} -> body; … end`. Struct-typed cases only,
// reusing emitTypeSwitchClauses. A default: becomes a trailing `_ ->`; without
// it a value matching no clause is an Erlang case_clause error (let-it-crash) —
// case never falls through, so it always yields a value or crashes.
func (em *emitter) emitTypeSwitchValue(ts *ast.TypeSwitchStmt) (string, error) {
	ta := ts.Assign.(*ast.AssignStmt).Rhs[0].(*ast.TypeAssertExpr)
	operand, err := em.emitExpr(ta.X)
	if err != nil {
		return "", err
	}
	clauses, err := em.emitTypeSwitchClauses(ts)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("case " + operand + " of\n")
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
```

Then reduce `emitTypeSwitchReceive` to just the wrapper (its gate/alias/init pre-work now lives in `emitTypeSwitch`):

```go
// emitTypeSwitchReceive wraps the shared clauses in a multi-clause `receive`.
// Precondition: em.tsAlias is set and the operand is otp.Receive() (guaranteed
// by emitTypeSwitch, which dispatches here only when isReceiveTypeSwitch holds).
func (em *emitter) emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error) {
	clauses, err := em.emitTypeSwitchClauses(ts)
	if err != nil {
		return "", err
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
```

Finally, in `emitStmts` change the type-switch dispatch (~line 351) from `em.emitTypeSwitchReceive(ts)` to `em.emitTypeSwitch(ts)`:

```go
			e, err := em.emitTypeSwitch(ts)
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'TypeSwitch' -count=1 -v`
Expected: PASS — the new value tests and every existing receive test.

- [x] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -m "feat: lower plain-value type switch to Erlang case X of"
```

---

### Task 3: Value path inherits every guard; message + comment cleanup

The value path routes through `emitTypeSwitchClauses` and the `emitTypeSwitch` entry, so it already inherits tag-collision rejection, bare-alias rejection, non-struct rejection, multi-type rejection, and init rejection. Lock that in with tests (these prove the refactor did not leave the value path unguarded — expect PASS immediately). Then bump the now-stale `(0.3.5+)` deferral strings to `(0.3.6+)` and update the `terminates()` comment.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (version strings in `emitTypeSwitchClauses` multi-type error and `emitExpr` alias error; `terminates()` comment ~466–468)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–2. No new exported behaviour.

- [x] **Step 1: Write the lock tests**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestModule_TypeSwitchValueGuardsInherited(t *testing.T) {
	otpImport := "import \"go.muehmer.eu/wintermute/pkg/otp\"\n"
	structs := "type Ping struct{ Data int }\ntype Pong struct{ Data int }\n"
	cases := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "tag collision Ping and *Ping",
			body:    "switch V := M.(type) {\ncase Ping:\notp.Print(V.Data)\ncase *Ping:\notp.Print(V.Data)\n}",
			wantSub: "same message tag",
		},
		{
			name:    "bare alias",
			body:    "switch V := M.(type) {\ncase Ping:\notp.Print(V)\n}",
			wantSub: "must be used via field access",
		},
		{
			name:    "non-struct case",
			body:    "switch V := M.(type) {\ncase int:\notp.Print(1)\n}",
			wantSub: "must name a struct type",
		},
		{
			name:    "multi-type case",
			body:    "switch V := M.(type) {\ncase Ping, Pong:\notp.Print(1)\n}",
			wantSub: "multi-type case is unsupported",
		},
		{
			name:    "init statement",
			body:    "switch N := f(); V := M.(type) {\ncase Ping:\notp.Print(V.Data)\n}",
			wantSub: "init statement is unsupported",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package m\n" + otpImport + structs +
				"func f() any { return 0 }\n" +
				"func Classify(M any) {\n" + tc.body + "\n}"
			_, err := Module(src)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("want %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestModule_TypeSwitchValueAsBareIfThen(t *testing.T) {
	// A value type switch terminates (case never falls through), so it may be a
	// bare-if then-branch — terminates() must accept it.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any, Flag bool) {
	if Flag {
		switch V := M.(type) {
		case Ping:
			otp.Print(V.Data)
		}
	}
	otp.Print(0)
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "case M of") {
		t.Errorf("want value switch inside bare-if then-branch:\n%s", r.Erl)
	}
}
```

> Match `r.Erl` / `f() any` to the codebase: confirm the `Result` field name and that a zero-arg helper returning `any` is the lightest way to give the init-statement case a valid RHS. If the existing tests use a different idiom for an init statement, mirror it.

- [x] **Step 2: Run the lock tests**

Run: `go test ./internal/pkg/transpile/ -run 'TypeSwitchValueGuards|TypeSwitchValueAsBareIf' -count=1 -v`
Expected: PASS for every sub-case — the value path already inherits each guard, and `terminates()` already accepts a type switch. (A FAIL means the refactor leaked a guard; fix before proceeding.)

- [x] **Step 3: Bump the deferred-feature version strings and update the comment**

In `transpile.go`:

- In `emitTypeSwitchClauses`, the multi-type error: `"multi-type case is unsupported (0.3.5+)"` → `"multi-type case is unsupported (0.3.6+)"`.
- In `emitExpr` (the `tsAlias` guard, ~line 919): `"passing the whole value is unsupported (0.3.5+)"` → `"passing the whole value is unsupported (0.3.6+)"`.
- In `terminates()` (the `*ast.TypeSwitchStmt` case comment, ~466–468), replace:

```go
		// A type-switch receive terminates once every clause terminates; unlike
		// a case-switch it needs NO default, because a receive cannot fall
		// through — it proceeds only on a match and yields that clause's value.
```

with:

```go
		// A type switch terminates once every clause terminates, needing NO
		// default: a receive proceeds only on a match, and a `case` never falls
		// through (it yields a matched clause's value or raises case_clause).
```

- [x] **Step 4: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/ -count=1`
Expected: PASS (message-text assertions, if any, still match — no test pins the `(0.3.5+)` substring; if one does, update it to `(0.3.6+)`).

- [x] **Step 5: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -m "test: lock value-path guards; bump deferred versions to 0.3.6+"
```

---

### Task 4: Runnable rung — end-to-end `erlc` + run

Prove the value type switch transpiles, compiles with `erlc`, and runs: call the transpiled function from the Erlang side with `{ping, 1}` and `{pong, 2}` and assert each clause fires.

**Files:**
- Create: `testdata/typeswitch/classify.go`
- Modify: `internal/pkg/ladder/ladder_integration_test.go` (add `TestRung_TypeSwitchValue`)

**Interfaces:**
- Consumes: the full transpile → `erlc` → run pipeline exercised by the existing `TestRung_TypeSwitchReceive` (0.3.4). Mirror its structure exactly (transpile the fixture, write the `.erl`, `erlc`, boot a node, call the function, capture output).

- [x] **Step 1: Read the existing rung to mirror it**

Run: `grep -n "TestRung_TypeSwitchReceive" internal/pkg/ladder/ladder_integration_test.go`
Then read that test and the `testdata/typeswitch/dispatch.go` fixture it drives — copy their structure (build tags, node boot, `erlc` invocation, output capture). Match the established pattern; do not invent a new harness.

- [x] **Step 2: Create the fixture**

Create `testdata/typeswitch/classify.go` (valid Go; uppercase operand/alias per the Erlang-variable rule; fields typed):

```go
package classify

import "go.muehmer.eu/wintermute/pkg/otp"

type Ping struct{ Data int }
type Pong struct{ Data int }

// Classify branches on the dynamic type of M and prints a tag-specific line.
func Classify(M any) {
	switch V := M.(type) {
	case Ping:
		otp.Print(V.Data)
	case Pong:
		otp.Print(V.Data)
	default:
		otp.Print(0)
	}
}
```

> Confirm the fixture package name and `otp.Print`'s Erlang lowering against `testdata/typeswitch/dispatch.go`; keep them identical in style. If the ladder harness calls an exported entry differently (e.g. a wrapper that starts a process), follow whatever `dispatch.go` does.

- [x] **Step 3: Write the failing integration test**

Add `TestRung_TypeSwitchValue` to `ladder_integration_test.go`, mirroring `TestRung_TypeSwitchReceive`. It transpiles `testdata/typeswitch/classify.go`, compiles with `erlc`, calls `classify:classify({ping, 1})` and `classify:classify({pong, 2})` from the Erlang side, and asserts the output contains `1` (ping clause) and `2` (pong clause). Use the exact node-boot / call idiom from the receive rung.

- [x] **Step 4: Run the integration test to verify it fails**

First clear any leftover BEAM nodes (integration-test gotcha):

```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
```

Run: `go test -tags integration -count=1 -run TestRung_TypeSwitchValue ./internal/pkg/ladder/ -v`
Expected: FAIL (fixture not yet wired / assertion mismatch — confirm it fails for the right reason, not a harness typo).

- [x] **Step 5: Make it pass**

Fix the fixture / test wiring until the transpiled module compiles and both clauses fire. If `erlc` errors, read the emitted `.erl` (the harness writes it to a temp dir) and reconcile with the receive rung's known-good output.

Run: `go test -tags integration -count=1 -run TestRung_TypeSwitchValue ./internal/pkg/ladder/ -v`
Expected: PASS.

- [x] **Step 6: Full verification**

```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
go test ./...
go test -tags integration -count=1 ./internal/pkg/ladder/
go test -tags integration -count=1 ./internal/pkg/cli/
```

Expected: all PASS.

- [x] **Step 7: Commit**

```bash
git add testdata/typeswitch/classify.go internal/pkg/ladder/ladder_integration_test.go
git commit -m "test: runnable rung for plain-value type switch (erlc + run)"
```

---

## Self-Review

**1. Spec coverage:**
- Value operand `X` → `case X of` — Task 2. ✓
- Struct cases only, tuple patterns — inherited via `emitTypeSwitchClauses`, Tasks 1–2. ✓
- Optional `default:` (with → `_ ->`, without → let-it-crash) — Task 2 (both tests). ✓
- `v.Field` binding, bare-`v` reject — inherited, locked in Task 3. ✓
- `terminates()` treats value switch as terminating — Task 3 (bare-if test + comment). ✓
- Out-of-scope rejections (non-struct, multi-type, whole-alias, init, tagless, non-tail) — Tasks 2–3. ✓
- Refactor into shared clause helper — Task 1. ✓
- Runnable rung — Task 4. ✓
- Deferred-version message bump (0.3.5+ → 0.3.6+) — Task 3. ✓

**2. Placeholder scan:** No TBD/TODO. Every code step shows the code. The `>` notes ask the implementer to confirm the `Result` field name and mirror the existing rung — these are verification instructions against real code, not deferred work.

**3. Type consistency:** `emitTypeSwitchClauses(ts) ([]string, error)` produced in Task 1, consumed unchanged in Task 2 (both wrappers). `emitTypeSwitch`/`emitTypeSwitchValue` signatures consistent between Task 2's Interfaces block and its code. `emitStmts` calls `emitTypeSwitch` (Task 2), matching the entry-point name. Operand extraction `ts.Assign.(*ast.AssignStmt).Rhs[0].(*ast.TypeAssertExpr).X` matches the AST reference.

**Open confirmation for the implementer** (flagged inline, not blocking): the `Result` Erlang-output field name (`r.Erl`) — verify against existing `transpile_test.go` assertions and match it throughout.
