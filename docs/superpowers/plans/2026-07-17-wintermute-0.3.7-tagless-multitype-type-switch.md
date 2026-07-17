# Wintermute 0.3.7 — tagless + multi-type type switch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the tagless type switch (`switch M.(type)`, no alias) and multi-type cases (`case Ping, Pong:`) to the transpiler, plus a fold fixing a false collision rejection for nested same-alias switches.

**Architecture:** Extend the shared clause builder. Multi-type expands one Go clause to N Erlang clauses (one per listed type) sharing a duplicated body, wildcarding struct fields (Go keeps `v` at the interface type). Tagless flows the same builder with `em.tsAlias == ""` (no alias/field binding; a throwaway guard variable for primitives). Both keep the four guards + struct tuples pairwise disjoint.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`, `maps`, `strings`).

## Global Constraints

- **Stdlib only** — no third-party modules, no new imports (`go/ast`, `go/token`, `maps`, `strings` already imported).
- **TDD red → green** — failing test first, run it, watch it fail, then implement.
- **`default:` REQUIRED for the value form** (tagless and multi-type alike) — a value matching no case falls through in Go, which a total Erlang `case` cannot express. Receive form keeps default optional.
- **Erlang variables uppercase** — the alias, when used as a variable, must be uppercase (`token.IsExported`); rejected otherwise.
- **New binding contexts integrate with `em.bound`** — snapshot/restore per clause, reject collisions.
- **Multi-type binds no fields** (Go forbids `v.Field` there); field access in a multi-type case is rejected loudly.
- **Wider primitive types stay rejected** (guard-collision hazard) — do NOT add map entries.
- **Module path** `go.muehmer.eu/wintermute`; test sources import `go.muehmer.eu/wintermute/pkg/otp`.
- **Build output** to `bin/`: `go build -o bin/wm ./cmd/wm`.

**Files (all tasks):**
- Modify: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go`
- Create (Task 4): `testdata/typeswitch/tagmulti/tagmulti.go`
- Modify (Task 4): `internal/pkg/ladder/ladder_integration_test.go`

**Reference — current code (post-0.3.6):**
- `emitTypeSwitch` (~580) rejects any non-`*ast.AssignStmt` `ts.Assign` ("type switch must bind an alias"); sets `em.tsAlias`; rejects init; dispatches `isReceiveTypeSwitch` → receive else value.
- `emitTypeSwitchValue` (~606) reads the operand via `ts.Assign.(*ast.AssignStmt).Rhs[0].(*ast.TypeAssertExpr)`.
- `isReceiveTypeSwitch` (~953) inspects `ts.Assign.(*ast.AssignStmt)` only.
- `emitTypeSwitchClauses` (~667) loops cases; rejects `len(cc.List) != 1` (multi-type); calls `caseTypeName` then `emitCaseClause(cc, name, guard, seenTag)` returning ONE clause string; default handling builds `defaultPattern` (`_` or the alias) and appends last.
- `emitCaseClause` (~751) single type: primitive → `V when guard(V)`; struct → `structPattern` (binds fields) + optional `V = ` prefix when `bodyUsesBareAlias`.
- `structPattern` (~833) binds all fields into `em.bound`; `registerAlias` (~855); `bodyUsesBareAlias` (~872) two-pass skip-set; `caseTypeName` (~799) → `(name, guard, err)`.

---

### Task 1: The nested-alias fold

Fix the 0.3.6 gate MINOR: `bodyUsesBareAlias` walks nested statements, so a bare `V` inside a nested `switch V := … .(type)` (same alias name) is misattributed to the outer alias, causing a false collision rejection.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`bodyUsesBareAlias` ~872; add `shadowsAlias`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `func shadowsAlias(n ast.Node, alias string) bool` — true iff `n` is a nested type switch rebinding `alias`.
- Consumes: existing `bodyUsesBareAlias`.

- [ ] **Step 1: Write the failing test**

```go
func TestModule_TypeSwitchNestedSameAliasNoFalseCollision(t *testing.T) {
	// An outer struct case whose body contains a nested type switch reusing the
	// same alias name V (valid Go shadowing) must NOT be rejected: the nested V is
	// the inner switch's binding, not a bare use of the outer alias.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping:
		switch V := V.Data.(type) {
		case int:
			otp.Print(V)
		default:
			otp.Print(0)
		}
	default:
		otp.Print(0)
	}
}`
	if _, err := Module(src); err != nil {
		t.Fatalf("nested same-alias switch must not be a false collision, got: %v", err)
	}
}
```

- [ ] **Step 2: Run it — verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchNestedSameAliasNoFalseCollision -v`
Expected: FAIL — the outer clause sees the inner bare `V` (via `bodyUsesBareAlias`), registers alias `V`, then the inner switch's `registerAlias` finds `V` already bound → false "collides with an already-bound name".

- [ ] **Step 3: Add `shadowsAlias` and prune both passes of `bodyUsesBareAlias`**

Add the helper (near `bodyUsesBareAlias`):

```go
// shadowsAlias reports whether n is a nested type switch that rebinds `alias`
// (binds the same name), so a bare alias reference inside its body belongs to
// the inner switch, not the one being scanned — matching how emitExpr resolves
// against the innermost em.tsAlias.
func shadowsAlias(n ast.Node, alias string) bool {
	ts, ok := n.(*ast.TypeSwitchStmt)
	if !ok {
		return false
	}
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 {
		return false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	return ok && id.Name == alias
}
```

In `bodyUsesBareAlias`, add a prune at the top of BOTH `ast.Inspect` closures (before the existing checks):

```go
	// pass 1 closure — add first:
			if shadowsAlias(n, alias) {
				return false // inner switch rebinds the alias; its body is the inner's scope
			}
	// pass 2 closure — add first:
			if shadowsAlias(n, alias) {
				return false
			}
```

(Add a one-line comment on the helper noting the residual: a nested same-alias switch whose *operand* references the outer alias would emit a loud unbound-variable error — astronomically rare, never silent.)

- [ ] **Step 4: Run it — verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchNestedSameAliasNoFalseCollision -v`
Expected: PASS

- [ ] **Step 5: Full package + commit**

Run: `go test ./internal/pkg/transpile/ -v` (all green — a nested *different*-alias switch still sees the outer `V`, unchanged).

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "fix: bodyUsesBareAlias stops at a nested type switch rebinding the same alias (0.3.6 gate MINOR)"
```

---

### Task 2: Multi-type cases

Deliver `case Ping, Pong:` — one Go clause → N Erlang clauses sharing a body, fields wildcarded, whole-alias V reused, field access rejected. Aliased switches only (tagless is Task 3).

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitTypeSwitchClauses` caller, `emitCaseClause`; add `structWildcardPattern`, `bodyUsesAliasField`, const `tsThrowaway`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces:
  - `func (em *emitter) emitCaseClause(cc *ast.CaseClause, seenTag map[string]bool) ([]string, error)` — now returns one clause per listed type.
  - `func (em *emitter) structWildcardPattern(typeName string) string` — `{ping, _, _}` (no `em.bound` mutation).
  - `func bodyUsesAliasField(body []ast.Stmt, alias string) bool`.
  - `const tsThrowaway = "_X"`.
- Consumes: `caseTypeName`, `structPattern`, `registerAlias`, `bodyUsesBareAlias`.

- [ ] **Step 1: Write the failing test — multi-type expansion + whole-alias**

```go
func TestModule_TypeSwitchMultiType(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping, Pong:
		otp.Send(V, V)
	case int, string:
		otp.Print(V)
	default:
		otp.Print(0)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"V = {ping, _} ->",
		"V = {pong, _} ->",
		"V when is_integer(V) ->",
		"V when is_binary(V) ->",
		"_ ->",
	} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q in:\n%s", want, r.Erl)
		}
	}
}
```

- [ ] **Step 2: Run it — verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchMultiType -v`
Expected: FAIL — `emitTypeSwitchClauses` rejects `len(cc.List) != 1` ("multi-type case is unsupported").

- [ ] **Step 3: Add helpers**

```go
// tsThrowaway is the Erlang variable a primitive case guards when the value is
// not bound to the alias (tagless, or a multi-type/unused body). The underscore
// prefix suppresses the "unused variable" warning; a bare _ is illegal in a guard.
const tsThrowaway = "_X"

// structWildcardPattern returns the Erlang tuple pattern for a declared struct
// with every field wildcarded ({ping, _, _}) — matches the tag and arity without
// binding anything. Used where fields cannot be bound: multi-type and tagless
// cases. Precondition: typeName is a declared struct (caseTypeName checked it).
func (em *emitter) structWildcardPattern(typeName string) string {
	parts := []string{strings.ToLower(typeName)}
	for range em.structs[typeName] {
		parts = append(parts, "_")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// bodyUsesAliasField reports whether the body accesses a field of the alias
// (alias.Field). A multi-type case forbids it — Go keeps v at the interface type
// there, and emitting the field name would reference an unbound (or unrelated,
// silently wrong) Erlang variable.
func bodyUsesAliasField(body []ast.Stmt, alias string) bool {
	found := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
					found = true
				}
			}
			return true
		})
	}
	return found
}
```

- [ ] **Step 4: Rewrite `emitCaseClause` to expand the type list**

Replace `emitCaseClause` with the list-expanding version. Note `bindFields := !isMulti` here (an aliased single struct still binds fields); Task 3 tightens it to `!isMulti && em.tsAlias != ""` for tagless.

```go
// emitCaseClause emits the Erlang clauses for one non-default type-switch case: a
// single-type case yields one clause, a multi-type case (case A, B:) yields one
// clause per listed type, all sharing the (duplicated) body. Fields are bound only
// for a single-type struct case (structPattern); a multi-type case wildcards
// struct fields and never binds fields (Go keeps v at the interface type there, so
// v.Field is rejected). The alias binds the whole value (V = {tag,_} / V when
// guard(V)) when the body uses bare V; otherwise struct fields wildcard and a
// primitive guards the tsThrowaway variable. Each listed type is deduped on its
// runtime discriminant; em.bound is snapshotted/restored around the clause.
func (em *emitter) emitCaseClause(cc *ast.CaseClause, seenTag map[string]bool) ([]string, error) {
	isMulti := len(cc.List) > 1

	if isMulti && bodyUsesAliasField(cc.Body, em.tsAlias) {
		return nil, em.errorf(cc, "field access is not allowed in a multi-type case; v keeps the interface type (bare v is allowed, v.Field is not)")
	}

	// Classify + dedup every listed type before emitting.
	type caseType struct {
		name, guard string
		at          ast.Expr
	}
	var types []caseType
	for _, e := range cc.List {
		name, guard, err := em.caseTypeName(e)
		if err != nil {
			return nil, err
		}
		key := "t:" + strings.ToLower(name)
		if guard != "" {
			key = "g:" + guard
		}
		if seenTag[key] {
			if guard != "" {
				return nil, em.errorf(e, "type switch has two cases of type %s; the second clause would be unreachable in Erlang", name)
			}
			return nil, em.errorf(e, "type switch has two cases with the same message tag %q; the second clause would be unreachable in Erlang (e.g. Ping and *Ping, or names differing only in case)", strings.ToLower(name))
		}
		seenTag[key] = true
		types = append(types, caseType{name, guard, e})
	}

	bindFields := !isMulti
	bindAlias := em.tsAlias != "" && bodyUsesBareAlias(cc.Body, em.tsAlias)

	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()

	if bindAlias {
		if err := em.registerAlias(cc); err != nil {
			return nil, err
		}
	}

	// Build one pattern per listed type.
	var pats []string
	for _, ct := range types {
		if ct.guard != "" { // primitive
			v := tsThrowaway
			if bindAlias {
				v = em.tsAlias
			}
			pats = append(pats, v+" when "+ct.guard+"("+v+")")
			continue
		}
		var pat string
		if bindFields {
			p, err := em.structPattern(ct.name, ct.at)
			if err != nil {
				return nil, err
			}
			pat = p
		} else {
			pat = em.structWildcardPattern(ct.name)
		}
		if bindAlias {
			pat = em.tsAlias + " = " + pat
		}
		pats = append(pats, pat)
	}

	body, err := em.emitStmts(cc.Body, true)
	if err != nil {
		return nil, err
	}

	clauses := make([]string, 0, len(pats))
	for _, p := range pats {
		clauses = append(clauses, p+" -> "+body)
	}
	return clauses, nil
}
```

- [ ] **Step 5: Update the caller in `emitTypeSwitchClauses`**

Remove the `len(cc.List) != 1` reject and the inline `caseTypeName` call; the loop now delegates fully:

```go
		clausesForCase, err := em.emitCaseClause(cc, seenTag)
		if err != nil {
			return nil, false, err
		}
		clauses = append(clauses, clausesForCase...)
```

(Delete the old `if len(cc.List) != 1 { … }`, `name, guard, err := em.caseTypeName(...)`, and single-clause append.)

- [ ] **Step 6: Run the multi-type test — verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchMultiType -v`
Expected: PASS

- [ ] **Step 7: Add reject + dedup + no-bare-V tests**

```go
func TestModule_TypeSwitchMultiTypeRejects(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			name: "field access in multi-type case",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping, Pong:
		otp.Print(V.Data)
	default:
		otp.Print(0)
	}
}`,
			want: "field access is not allowed in a multi-type case",
		},
		{
			name: "duplicate type within the list",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping, Ping:
		otp.Send(V, V)
	default:
		otp.Print(0)
	}
}`,
			want: "same message tag",
		},
		{
			name: "duplicate type across a list and a later case",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping, Pong:
		otp.Send(V, V)
	case Pong:
		otp.Send(V, V)
	default:
		otp.Print(0)
	}
}`,
			want: "same message tag",
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

func TestModule_TypeSwitchMultiTypeNoBareAlias(t *testing.T) {
	// A multi-type case whose body ignores the value wildcards the struct fields
	// and guards the throwaway variable — no alias binding.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
type Pong struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping, Pong:
		otp.Print(1)
	case int, string:
		otp.Print(2)
	default:
		otp.Print(0)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"{ping, _} ->", "{pong, _} ->", "_X when is_integer(_X) ->", "_X when is_binary(_X) ->"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q in:\n%s", want, r.Erl)
		}
	}
}
```

- [ ] **Step 8: Run the full transpile suite — verify green**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS — including every 0.3.6 single-type test (single struct still binds fields via `structPattern`; single primitive still `V when …`).

- [ ] **Step 9: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat: multi-type type-switch cases (case A, B:) — expand to N clauses sharing a body, fields wildcarded, v.Field rejected"
```

---

### Task 3: Tagless form

Deliver `switch M.(type)` (no alias) in both value and receive forms, flowing the Task-2 builder with `em.tsAlias == ""`.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitTypeSwitch`, `emitTypeSwitchValue`, `isReceiveTypeSwitch`; add `typeSwitchAssert`; tighten `bindFields` in `emitCaseClause`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces: `func typeSwitchAssert(ts *ast.TypeSwitchStmt) (*ast.TypeAssertExpr, bool)` — the `X.(type)` assertion of an aliased or tagless switch.
- Consumes: Task-2 `emitCaseClause` (already handles wildcard/throwaway).

- [ ] **Step 1: Write the failing test — tagless value form**

```go
func TestModule_TypeSwitchTagless(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch M.(type) {
	case Ping:
		otp.Print(1)
	case int:
		otp.Print(2)
	default:
		otp.Print(0)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"case M of", "{ping, _} ->", "_X when is_integer(_X) ->", "_ ->"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q in:\n%s", want, r.Erl)
		}
	}
}
```

- [ ] **Step 2: Run it — verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchTagless -v`
Expected: FAIL — `emitTypeSwitch` rejects the non-`AssignStmt` `ts.Assign` ("type switch must bind an alias").

- [ ] **Step 3: Add `typeSwitchAssert` and accept the tagless entry**

Add the helper:

```go
// typeSwitchAssert returns the `X.(type)` assertion of a type switch, whether it
// is aliased (v := X.(type); ts.Assign an *ast.AssignStmt) or tagless (X.(type);
// ts.Assign an *ast.ExprStmt).
func typeSwitchAssert(ts *ast.TypeSwitchStmt) (*ast.TypeAssertExpr, bool) {
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if a.Tok == token.DEFINE && len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				return ta, true
			}
		}
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok {
			return ta, true
		}
	}
	return nil, false
}
```

Rewrite `emitTypeSwitch` to accept both forms:

```go
func (em *emitter) emitTypeSwitch(ts *ast.TypeSwitchStmt) (string, error) {
	if ts.Init != nil {
		return "", em.errorf(ts, "type switch with an init statement is unsupported (0.3.8+)")
	}
	alias := ""
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return "", em.errorf(ts, "unsupported type switch binding")
		}
		id, ok := a.Lhs[0].(*ast.Ident)
		if !ok {
			return "", em.errorf(ts, "type switch alias must be a single identifier")
		}
		alias = id.Name
	case *ast.ExprStmt:
		// tagless: no alias
	default:
		return "", em.errorf(ts, "unsupported type switch form")
	}
	old := em.tsAlias
	em.tsAlias = alias
	defer func() { em.tsAlias = old }()
	if isReceiveTypeSwitch(ts) {
		return em.emitTypeSwitchReceive(ts)
	}
	return em.emitTypeSwitchValue(ts)
}
```

Update `emitTypeSwitchValue`'s operand extraction:

```go
	ta, _ := typeSwitchAssert(ts)
	operand, err := em.emitExpr(ta.X)
```

Update `isReceiveTypeSwitch` to use the helper:

```go
func isReceiveTypeSwitch(ts *ast.TypeSwitchStmt) bool {
	ta, ok := typeSwitchAssert(ts)
	if !ok || ta.Type != nil { // .(type), not .(T)
		return false
	}
	call, ok := ta.X.(*ast.CallExpr)
	return ok && isOtpCall(call, "Receive")
}
```

Tighten `bindFields` in `emitCaseClause` so a tagless struct case wildcards (does not bind fields):

```go
	bindFields := !isMulti && em.tsAlias != ""
```

- [ ] **Step 4: Run the tagless test — verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchTagless -v`
Expected: PASS

- [ ] **Step 5: Add tagless receive + no-default reject tests**

```go
func TestModule_TypeSwitchTaglessReceive(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Serve() {
	switch otp.Receive().(type) {
	case Ping:
		otp.Print(1)
	case int:
		otp.Print(2)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "receive") || !strings.Contains(r.Erl, "{ping, _} ->") || !strings.Contains(r.Erl, "_X when is_integer(_X) ->") {
		t.Errorf("want tagless receive clauses:\n%s", r.Erl)
	}
}

func TestModule_TypeSwitchTaglessValueNoDefaultRejected(t *testing.T) {
	// A tagless VALUE switch with no default falls through in Go; reject it.
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
	if err == nil || !strings.Contains(err.Error(), "requires a default clause") {
		t.Fatalf("want default-required rejection, got %v", err)
	}
}
```

- [ ] **Step 6: Update the obsolete tagless-rejected test**

`TestModule_TypeSwitchTaglessRejected` (asserts `switch M.(type)` errors "must bind an alias") is now obsolete — tagless is supported. **Delete that test.** (The tagless behavior is covered by the new tests above; the default-required guard is covered by `TestModule_TypeSwitchTaglessValueNoDefaultRejected`.)

- [ ] **Step 7: Run the full transpile suite — verify green**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (all new + existing; aliased single/multi cases unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat: tagless type switch (switch M.(type)) — no alias, wildcard struct + throwaway guard, value + receive forms"
```

---

### Task 4: Runnable rung — tagless + multi-type compiles and runs

**Files:**
- Create: `testdata/typeswitch/tagmulti/tagmulti.go`
- Modify: `internal/pkg/ladder/ladder_integration_test.go`
- Test: the integration ladder (`-tags integration`).

**Interfaces:** consumes the full transpiler (Tasks 1–3). No new Go symbols.

- [ ] **Step 1: Inspect the existing rung conventions**

Run: `grep -n "TestRung_TypeSwitchMixed\|transpileToErl\|classify_mixed" internal/pkg/ladder/ladder_integration_test.go`
Note the structure of `TestRung_TypeSwitchMixed` (NewLayout/Installed-skip/TempDir/transpileToErl/erlc/`erl -eval io:format("~p", …)`/TrimSpace). The module name is the fixture's package name (via `transpile.File`).

- [ ] **Step 2: Write the fixture `testdata/typeswitch/tagmulti/tagmulti.go`**

```go
// Package tagmulti is a 0.3.7 fixture exercising a multi-type case (case Ping,
// Pong:) and a primitive arm in one plain-value type switch. It transpiles,
// compiles with erlc, and runs to a checked result.
package tagmulti

type Ping struct{ Seq int }
type Pong struct{ Seq int }

// Classify returns 1 for a Ping or a Pong (multi-type case, whole value ignored),
// the int itself for an int, and 0 otherwise.
func Classify(M any) int {
	switch V := M.(type) {
	case Ping, Pong:
		return 1
	case int:
		return V
	default:
		return 0
	}
}
```

(The multi-type body returns a constant — a multi-type case cannot access fields. The `int` arm uses bare `V` to exercise whole-alias under a guard.)

- [ ] **Step 3: Add the ladder rung**

Add `TestRung_TypeSwitchTagMulti` mirroring `TestRung_TypeSwitchMixed`, pointing at `../../../testdata/typeswitch/tagmulti/tagmulti.go`, module `tagmulti`, calling `tagmulti:classify(...)`:

```go
for _, tc := range []struct{ send, want string }{
    {"{ping, 5}", "1"},   // multi-type case, either tag → 1
    {"{pong, 9}", "1"},
    {"7", "7"},           // int arm → whole-alias V
    {"an_atom", "0"},     // default
} {
```

Same `io:format("~p", …)`/`init:stop()`/TrimSpace/compare pattern and a doc comment matching the neighboring rungs (0.3.7 tagless/multi-type rung — here exercising multi-type + primitive + default; a tagless variant can be folded in if the harness stays simple).

- [ ] **Step 4: Run it (clean BEAM first)**

```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
go build -o bin/wm ./cmd/wm
go test -tags integration -count=1 ./internal/pkg/ladder/ -run TypeSwitch -v
```
Expected: `TestRung_TypeSwitchTagMulti` PASS (and existing TypeSwitch rungs pass). `-count=1` (transpile.go changed). Re-clean on any epmd/beam.smp flake.

- [ ] **Step 5: Full integration suites (no regression)**

```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
go test -tags integration -count=1 ./internal/pkg/ladder/ ./internal/pkg/cli/
```
Expected: both PASS. Also `go test ./...` (unit) green.

- [ ] **Step 6: Commit**

```bash
git add testdata/typeswitch/tagmulti/tagmulti.go internal/pkg/ladder/
git commit -s -m "test: runnable rung — multi-type + primitive type switch compiles and runs"
```

---

## After all tasks

- Clean BEAM state, then `go test ./...` + both integration suites once more.
- Release ritual per `HANDOVER.md` (VERSION bump, squash → `main`, **Copilot gate on the release diff**, push origin→upstream→github, GitHub release, `production-0.3.7`, handover/README refresh). The 0.3.7 gate template: (a) the multi-type field-access reject (verify a `v.Field` in a multi-type case is rejected, not emitted); (b) the tagless throwaway guard `_X` and wildcard struct patterns compile (verify with the rung); (c) `default:` required for both tagless and multi-type value forms; (d) disjointness unchanged.

## Self-review notes (author)

- **Spec coverage:** tagless value+receive (Task 3) ✓; struct→`{tag,_}` wildcard + primitive→`_X when …` throwaway (Tasks 2–3) ✓; multi-type expansion + shared body (Task 2) ✓; whole-alias V reuse (Task 2) ✓; multi-type v.Field reject (Task 2 Step 7) ✓; dedup over the list (Task 2 Step 7) ✓; default required for value form, both forms (Task 3 Step 5) ✓; the fold (Task 1) ✓; runnable rung (Task 4) ✓; wider primitives NOT added (constraint) ✓.
- **Type consistency:** `emitCaseClause` becomes `(cc, seenTag) ([]string, error)` in Task 2; its sole caller is updated in the same task (Task 2 Step 5). `bindFields` is `!isMulti` in Task 2, tightened to `!isMulti && em.tsAlias != ""` in Task 3 (same function, additive). `typeSwitchAssert` (Task 3) is consumed by `emitTypeSwitchValue`/`isReceiveTypeSwitch` in the same task. `tsThrowaway`/`structWildcardPattern`/`bodyUsesAliasField`/`shadowsAlias` signatures stable.
- **Ordering:** the fold (Task 1) is independent; multi-type (Task 2) lands the wildcard/throwaway machinery that tagless (Task 3) reuses by flipping the alias conditions.
