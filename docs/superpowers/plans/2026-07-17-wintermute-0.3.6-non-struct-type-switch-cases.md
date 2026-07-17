# Wintermute 0.3.6 — non-struct type-switch cases + whole-alias V — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Widen the type switch to accept non-struct cases over Go primitive types (`int`/`string`/`bool`/`float64` → Erlang type guards) and bind the switch alias `V` to the whole matched value.

**Architecture:** Extend the shared clause builder `emitTypeSwitchClauses` (used by both the value form `case X of` and the receive form `receive`). A case is either a struct case (tuple pattern, optionally aliased `V = {tag,Fields}`) or a primitive case (the alias variable guarded by `is_integer`/`is_binary`/`is_boolean`/`is_float`). The alias becomes a real bound Erlang variable via a new `registerAlias` helper; `emitExpr` stops rejecting a bare alias reference and lowers it to the Erlang variable.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`, `maps`, `strings`). No third-party modules.

## Global Constraints

- **Stdlib only** — no third-party modules. `go/ast`, `go/token`, `maps`, `strings` are all already imported in `transpile.go`; add no imports.
- **TDD red → green** — write the failing test, run it, watch it fail, then implement.
- **main() → run() pattern** — not touched here; all work is in `internal/pkg/transpile/`.
- **`default:` stays REQUIRED for the value form** — unchanged from 0.3.5. A value matching no case falls through in Go but a total Erlang `case` raises `case_clause`.
- **Erlang variables are uppercase** — the alias, when used as a variable, must be uppercase-leading (`token.IsExported`); a lowercase alias is rejected, never emitted as an atom.
- **New binding contexts integrate with `em.bound`** — snapshot/restore around each clause; reject collisions with an already-bound name.
- **Module path** is `go.muehmer.eu/wintermute` (test sources import `go.muehmer.eu/wintermute/pkg/otp`).
- **Build output** to `bin/`: `go build -o bin/wm ./cmd/wm`.

**Files (all tasks):**
- Modify: `internal/pkg/transpile/transpile.go`
- Test: `internal/pkg/transpile/transpile_test.go`
- Create (Task 3): `testdata/typeswitch/classify_mixed.go`
- Modify (Task 3): `internal/pkg/ladder/ladder_integration_test.go` (or the existing ladder table — confirm the file name at Task 3)

**Reference — current clause builder** (`transpile.go` ~664–718), struct-only:
`emitTypeSwitchClauses` loops `ts.Body.List`, rejects non-`CaseClause`, empty body, and multi-clause `default`; `len(cc.List) != 1` → multi-type reject; `caseTypeName(cc.List[0])` → struct name; `seenTag[strings.ToLower(name)]` dedup; `structPattern` binds fields in a snapshotted `em.bound`; `em.emitStmts(cc.Body, true)` → body; appends `pat+" -> "+body`. `emitExpr` (~984–999) currently rejects a bare alias ident (`must be used via field access`).

---

### Task 1: whole-alias V for struct cases (alias-as-variable foundation)

Deliver `V = {tag, Fields}` for struct cases whose body uses bare `V`, while a field-only struct case stays byte-identical to 0.3.5 output. Introduce the alias-as-Erlang-variable machinery (`registerAlias`, `bodyUsesBareAlias`, `emitExpr` change) that Task 2 reuses.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr` ~984–999; extract `emitCaseClause`; add `registerAlias`, `bodyUsesBareAlias`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Produces:
  - `func (em *emitter) registerAlias(at ast.Node) error` — validates the alias is uppercase, rejects a collision with an already-bound name, and registers `em.tsAlias` in `em.bound`.
  - `func bodyUsesBareAlias(body []ast.Stmt, alias string) bool` — true iff some statement references `alias` as a bare identifier (not as `alias.Field`).
  - `func (em *emitter) emitCaseClause(cc *ast.CaseClause, name string, seenTag map[string]bool) (string, error)` — emits one non-default struct clause, snapshotting `em.bound`.
- Consumes: existing `structPattern`, `caseTypeName`, `emitStmts`, `em.tsAlias`, `em.bound`.

- [ ] **Step 1: Write the failing test — struct whole-alias binds the tuple**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestModule_TypeSwitchStructWholeAlias(t *testing.T) {
	// A struct case whose body uses bare V (not just V.Field) binds the whole
	// tuple with an Erlang alias pattern: V = {ping, Data}.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case Ping:
		otp.Send(V.Data, V)
	default:
		otp.Print(0)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "V = {ping, Data} ->") {
		t.Errorf("want whole-alias pattern `V = {ping, Data} ->`:\n%s", r.Erl)
	}
}
```

- [ ] **Step 2: Run it — verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchStructWholeAlias -v`
Expected: FAIL — currently bare `V` is rejected (`must be used via field access`), so `Module` returns an error.

- [ ] **Step 3: Add `registerAlias` and `bodyUsesBareAlias`**

Add both helpers to `transpile.go` (near `structPattern`, ~771):

```go
// registerAlias binds the type-switch alias as an Erlang variable in the current
// (snapshotted) bound scope. It rejects a lowercase alias (which would emit an
// Erlang atom, not a variable) and a collision with an already-bound name (a
// parameter, a prior :=, or a struct field — in Erlang a rebind is an equality
// match, not a fresh binding). Call it only when the clause actually uses the
// alias as an Erlang variable.
func (em *emitter) registerAlias(at ast.Node) error {
	a := em.tsAlias
	if !token.IsExported(a) {
		return em.errorf(at, "type-switch alias %s is lowercase-leading; Erlang variables must be uppercase", a)
	}
	if em.bound[a] {
		return em.errorf(at, "type-switch alias %s collides with an already-bound name (a parameter, a prior :=, or a struct field); Erlang would treat it as an equality match, not a fresh binding — rename one", a)
	}
	em.bound[a] = true
	return nil
}

// bodyUsesBareAlias reports whether any statement in body references the
// type-switch alias as a whole value (a bare identifier) rather than only via
// field access (alias.Field). It drives whether a struct clause binds the whole
// tuple (V = {tag, Fields}); emitExpr lowers exactly the same bare references to
// the Erlang variable, so detection and emission stay in lockstep.
func bodyUsesBareAlias(body []ast.Stmt, alias string) bool {
	used := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if used {
				return false
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
					return false // alias.Field is not a bare use; do not descend
				}
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == alias {
				used = true
			}
			return true
		})
	}
	return used
}
```

- [ ] **Step 4: Extract `emitCaseClause` and wire the whole-alias prefix**

In `transpile.go`, replace the per-case body of the loop in `emitTypeSwitchClauses` (the block from `name, err := em.caseTypeName(...)` through `clauses = append(clauses, pat+" -> "+body)`, ~692–712) with a call:

```go
		name, err := em.caseTypeName(cc.List[0])
		if err != nil {
			return nil, false, err
		}
		clause, err := em.emitCaseClause(cc, name, seenTag)
		if err != nil {
			return nil, false, err
		}
		clauses = append(clauses, clause)
```

Add the new method (after `emitTypeSwitchClauses`):

```go
// emitCaseClause emits one non-default type-switch clause "Pattern -> Body" for a
// struct case. The pattern is the struct tuple {tag, Fields}; when the body uses
// the alias as a whole value (bodyUsesBareAlias) it is bound too, as an Erlang
// alias pattern V = {tag, Fields}. Snapshots/restores em.bound around the clause.
func (em *emitter) emitCaseClause(cc *ast.CaseClause, name string, seenTag map[string]bool) (string, error) {
	tag := strings.ToLower(name)
	if seenTag[tag] {
		return "", em.errorf(cc.List[0], "type switch has two cases with the same message tag %q; the second clause would be unreachable in Erlang (e.g. Ping and *Ping, or names differing only in case)", tag)
	}
	seenTag[tag] = true

	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()

	pat, err := em.structPattern(name, cc)
	if err != nil {
		return "", err
	}
	if bodyUsesBareAlias(cc.Body, em.tsAlias) {
		if err := em.registerAlias(cc); err != nil {
			return "", err
		}
		pat = em.tsAlias + " = " + pat
	}
	body, err := em.emitStmts(cc.Body, true)
	if err != nil {
		return "", err
	}
	return pat + " -> " + body, nil
}
```

Note: the old loop manually restored `em.bound` on each path; `emitCaseClause` now owns that via `defer`. Remove the now-unused `snap`/`em.bound = snap` handling from the loop body if any remains.

- [ ] **Step 5: Change `emitExpr` to lower a bare alias to the Erlang variable**

In `transpile.go` `emitExpr`, the `*ast.Ident` case (~984–999), delete the bare-alias reject block:

```go
		// DELETE these lines (~985–991):
		// The active type-switch alias (e.g. `v` in `switch v := otp.Receive().(type)`)
		// must be used via field access (v.Field); the alias itself has no direct
		// Erlang representation (each case binds different fields), so passing the
		// whole value is unsupported.
		if em.tsAlias != "" && ex.Name == em.tsAlias {
			return "", em.errorf(ex, "the type-switch alias %s must be used via field access (%s.Field); passing the whole value is unsupported (0.3.6+)", ex.Name, ex.Name)
		}
```

The remaining `token.IsExported` check then handles a bare alias: uppercase `V` returns `"V"` (bound by the clause), lowercase `v` is rejected (`must be uppercase`). Update the preceding comment to reflect that a bare alias is now emitted as its Erlang variable when the clause binds it.

- [ ] **Step 6: Run the whole-alias test — verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchStructWholeAlias -v`
Expected: PASS

- [ ] **Step 7: Update the two obsolete "bare alias" reject tests**

Both existing reject tables assert bare `V` is rejected; whole-alias now makes it valid. Edit `transpile_test.go`:

- In `TestModule_TypeSwitchRejects` (~1546–1558), **delete** the `"bare alias"` case (the receive-form `otp.Send(v.Data, v)` one).
- In `TestModule_TypeSwitchValueGuardsInherited` (~1712–1716), **delete** the `"bare alias"` case (`otp.Print(V)`).

Add a positive receive-form whole-alias test:

```go
func TestModule_TypeSwitchReceiveWholeAlias(t *testing.T) {
	// The receive form also binds the whole alias when the body uses bare V.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Serve() {
	switch V := otp.Receive().(type) {
	case Ping:
		otp.Send(V.Data, V)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "V = {ping, Data} ->") {
		t.Errorf("want whole-alias in receive clause:\n%s", r.Erl)
	}
}
```

- [ ] **Step 8: Write failing rejects — lowercase alias + collision**

Add to `transpile_test.go`:

```go
func TestModule_TypeSwitchWholeAliasRejects(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			// A lowercase alias used as a whole value would emit an Erlang atom,
			// not a variable — reject it.
			name: "lowercase alias whole use",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch v := M.(type) {
	case Ping:
		otp.Send(v.Data, v)
	default:
		otp.Print(0)
	}
}`,
			want: "must be uppercase",
		},
		{
			// The alias collides with a parameter named V; a rebind in Erlang is
			// an equality match, not a fresh binding.
			name: "alias collides with param",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any, V int) {
	switch V := M.(type) {
	case Ping:
		otp.Send(V.Data, V)
	default:
		otp.Print(0)
	}
}`,
			want: "collides with an already-bound name",
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

- [ ] **Step 9: Run the full transpile suite — verify green**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS — new whole-alias + reject tests pass; the edited reject tables no longer reference bare-alias; every prior test still green (field-only struct cases emit `{ping, Data} ->` unchanged).

- [ ] **Step 10: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat: type-switch whole-alias V (struct cases) — bind V = {tag, Fields} when the body uses the whole value"
```

---

### Task 2: primitive (non-struct) cases → Erlang type guards

Deliver `case int:`/`string:`/`bool:`/`float64:` in both the value and receive forms, lowered to guarded alias clauses. Reuses `registerAlias` from Task 1.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`caseTypeName`, `emitCaseClause`; add `primitiveGuard`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `registerAlias`, `emitCaseClause`, `structPattern`, `bodyUsesBareAlias` (Task 1).
- Produces:
  - `var primitiveGuard map[string]string` — case type name → Erlang guard.
  - `func (em *emitter) caseTypeName(e ast.Expr) (name, guard string, err error)` — `guard == ""` for a struct, non-empty for a primitive.
  - `emitCaseClause(cc *ast.CaseClause, name, guard string, seenTag map[string]bool)` — extended with `guard`; a primitive clause is `V when <guard>(V) -> Body`.

- [ ] **Step 1: Write the failing test — value-form primitive guard**

Add to `transpile_test.go`:

```go
func TestModule_TypeSwitchValuePrimitiveGuards(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Ping struct{ Data int }
func Classify(M any) {
	switch V := M.(type) {
	case int:
		otp.Print(V)
	case string:
		otp.Print(V)
	case Ping:
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
		"V when is_integer(V) ->",
		"V when is_binary(V) ->",
		"{ping, Data} ->",
		"_ ->",
	} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q in:\n%s", want, r.Erl)
		}
	}
}
```

- [ ] **Step 2: Run it — verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchValuePrimitiveGuards -v`
Expected: FAIL — `caseTypeName` rejects `int`/`string` (`must name a struct type`).

- [ ] **Step 3: Add `primitiveGuard` and extend `caseTypeName`**

In `transpile.go`, add the table (package level, near the emitter type):

```go
// primitiveGuard maps a supported non-struct type-switch case type to its Erlang
// type guard. The four guards are pairwise disjoint (and disjoint from
// atom-tagged struct tuples), so Erlang's runtime first-match order coincides
// with Go's static per-case type exclusivity.
var primitiveGuard = map[string]string{
	"int":     "is_integer",
	"string":  "is_binary",
	"bool":    "is_boolean",
	"float64": "is_float",
}
```

Replace `caseTypeName` (~726–738) with the classifying version:

```go
// caseTypeName classifies a single type-switch case expression, accepting both
// `T` and `*T` (Erlang has no pointers, so the star is meaningless). For a
// declared struct it returns (name, "", nil); for a supported primitive it
// returns (name, guard, nil); anything else is rejected.
func (em *emitter) caseTypeName(e ast.Expr) (name, guard string, err error) {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", "", em.errorf(e, "type switch case must name a type")
	}
	if _, ok := em.structs[id.Name]; ok {
		return id.Name, "", nil
	}
	if g, ok := primitiveGuard[id.Name]; ok {
		return id.Name, g, nil
	}
	return "", "", em.errorf(e, "unsupported type-switch case type %s; only declared structs and int/string/bool/float64 are supported (0.3.7+)", id.Name)
}
```

Update the caller in `emitTypeSwitchClauses` to the three-value return and pass `guard`:

```go
		name, guard, err := em.caseTypeName(cc.List[0])
		if err != nil {
			return nil, false, err
		}
		clause, err := em.emitCaseClause(cc, name, guard, seenTag)
		if err != nil {
			return nil, false, err
		}
		clauses = append(clauses, clause)
```

- [ ] **Step 4: Extend `emitCaseClause` with the primitive branch**

Replace `emitCaseClause` (from Task 1) with the guard-aware version:

```go
// emitCaseClause emits one non-default type-switch clause "Pattern[ Guard] -> Body".
// guard == "" is the struct form (tuple pattern {tag, Fields}, optionally aliased
// V = {tag, Fields} when the body uses the whole value); a non-empty guard is the
// primitive form (the alias variable guarded, e.g. V when is_integer(V)). It
// dedups on the clause's runtime discriminant (a lowercased struct tag or the
// guard name — these never collide, and a guard and a tuple are disjoint) and
// snapshots/restores em.bound around the clause.
func (em *emitter) emitCaseClause(cc *ast.CaseClause, name, guard string, seenTag map[string]bool) (string, error) {
	key := "t:" + strings.ToLower(name)
	if guard != "" {
		key = "g:" + guard
	}
	if seenTag[key] {
		if guard != "" {
			return "", em.errorf(cc.List[0], "type switch has two cases of type %s; the second clause would be unreachable in Erlang", name)
		}
		return "", em.errorf(cc.List[0], "type switch has two cases with the same message tag %q; the second clause would be unreachable in Erlang (e.g. Ping and *Ping, or names differing only in case)", strings.ToLower(name))
	}
	seenTag[key] = true

	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()

	if guard != "" { // primitive case: `V when is_T(V) -> Body`
		if err := em.registerAlias(cc); err != nil {
			return "", err
		}
		body, err := em.emitStmts(cc.Body, true)
		if err != nil {
			return "", err
		}
		return em.tsAlias + " when " + guard + "(" + em.tsAlias + ") -> " + body, nil
	}

	pat, err := em.structPattern(name, cc)
	if err != nil {
		return "", err
	}
	if bodyUsesBareAlias(cc.Body, em.tsAlias) {
		if err := em.registerAlias(cc); err != nil {
			return "", err
		}
		pat = em.tsAlias + " = " + pat
	}
	body, err := em.emitStmts(cc.Body, true)
	if err != nil {
		return "", err
	}
	return pat + " -> " + body, nil
}
```

- [ ] **Step 5: Run the primitive test — verify it passes**

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchValuePrimitiveGuards -v`
Expected: PASS

- [ ] **Step 6: Write the receive-form primitive test**

The shared builder gives the receive form primitives for free; lock it:

```go
func TestModule_TypeSwitchReceivePrimitiveGuard(t *testing.T) {
	// A non-struct case in the receive form is a guarded selective-receive clause
	// (it blocks on a non-match, never falls through — no default needed).
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Serve() {
	switch V := otp.Receive().(type) {
	case int:
		otp.Print(V)
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(r.Erl, "receive") || !strings.Contains(r.Erl, "V when is_integer(V) ->") {
		t.Errorf("want guarded receive clause:\n%s", r.Erl)
	}
}
```

Run: `go test ./internal/pkg/transpile/ -run TestModule_TypeSwitchReceivePrimitiveGuard -v`
Expected: PASS (no new implementation — regression lock).

- [ ] **Step 7: Update obsolete "non-struct case" reject tests + add new rejects**

Edit `transpile_test.go`:

- In `TestModule_TypeSwitchRejects` (~1520–1531), **delete** the `"non-struct case"` case (`case int:` is now valid).
- In `TestModule_TypeSwitchValueGuardsInherited` (~1717–1721), **delete** the `"non-struct case"` case.
- Update the `"multi-type case"` reject messages if they still say `(0.3.6+)`: the emitter message in `emitTypeSwitchClauses` should read `(0.3.7+)` (edit both the code string and any test `wantSub` that pins the version — the current tests match on `"multi-type case"` substring, so no test change needed, but bump the code comment/string).

Add a new reject table:

```go
func TestModule_TypeSwitchPrimitiveRejects(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			name: "unsupported primitive type",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Classify(M any) {
	switch V := M.(type) {
	case int32:
		otp.Print(V)
	default:
		otp.Print(0)
	}
}`,
			want: "unsupported type-switch case type int32",
		},
		{
			name: "duplicate primitive case",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Classify(M any) {
	switch V := M.(type) {
	case int:
		otp.Print(V)
	case int:
		otp.Print(V)
	default:
		otp.Print(0)
	}
}`,
			want: "two cases of type int",
		},
		{
			name: "value primitive switch without default",
			src: `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Classify(M any) {
	switch V := M.(type) {
	case int:
		otp.Print(V)
	}
}`,
			want: "requires a default clause",
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

- [ ] **Step 8: Run the full transpile suite — verify green**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (all new + existing tests).

- [ ] **Step 9: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat: non-struct type-switch cases (int/string/bool/float64) -> Erlang type guards, value + receive forms"
```

---

### Task 3: runnable rung — mixed value type switch transpiles, compiles, and runs

Deliver an integration fixture that mixes a primitive case, a struct case, and a default, proving the lowering compiles with `erlc` and runs correctly on `erl`.

**Files:**
- Create: `testdata/typeswitch/classify_mixed.go`
- Modify: the ladder integration test that enumerates `testdata/typeswitch/*` fixtures (confirm the exact file — Task 3 Step 1 locates it; existing rung is `testdata/typeswitch/classify.go` per the handover).
- Test: the integration ladder (`-tags integration`).

**Interfaces:**
- Consumes: the full transpiler (Tasks 1–2). No new Go symbols.

- [ ] **Step 1: Locate the ladder rung for the existing classify fixture**

Run: `grep -rn "typeswitch/classify\|classify" internal/pkg/ladder/`
Expected: find the integration test that transpiles `testdata/typeswitch/classify.go`, compiles it with `erlc`, calls it via `erl`, and asserts the result. Note its structure (function name, expected-output assertion) — the new rung mirrors it.

- [ ] **Step 2: Write the fixture `testdata/typeswitch/classify_mixed.go`**

The fixture must be within the transpiler subset (typed struct fields, uppercase Erlang vars, default present). Mirror `classify.go`'s shape (a self-callable exported function returning an int the ladder can assert):

```go
package classify_mixed

import "go.muehmer.eu/wintermute/pkg/otp"

type Ping struct{ Seq int }

// Classify returns 1 for an int, 2 for a string, the sequence for a Ping, and 0
// otherwise — exercising a primitive guard, a struct tuple, and the default in
// one value type switch.
func Classify(M any) int {
	switch V := M.(type) {
	case int:
		return V
	case string:
		return otp.ByteSize(V)
	case Ping:
		return V.Seq
	default:
		return 0
	}
}
```

**Adjust to the real fixture conventions found in Step 1** — package name, imports, and the exact helper used for the assertion. If `classify.go` does not import `otp` or call a helper like `otp.ByteSize`, drop the `string` arm's helper and return a literal (e.g. `case string: return 2`) so the fixture stays inside the subset. The goal is three arms + default that compile and run; keep it minimal.

- [ ] **Step 3: Add the ladder rung**

Add an entry mirroring the `classify.go` rung: transpile `classify_mixed.go`, `erlc`-compile the emitted module, and assert calls via `erl`:
- `Classify(7)` → `7`
- `Classify(<<"x">>)` (an Erlang binary) → the string arm's value
- `Classify({ping, 3})` → `3`
- `Classify(other)` → `0`

Follow the exact call/assert mechanism the existing rung uses (Step 1). Keep the assertions to what the harness already supports.

- [ ] **Step 4: Clear any leftover BEAM nodes, then run the integration ladder**

Run:
```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
go build -o bin/wm ./cmd/wm
go test -tags integration -count=1 ./internal/pkg/ladder/ -run TypeSwitch -v
```
Expected: PASS — `classify_mixed` transpiles, compiles with `erlc`, and each call returns the asserted value. (`-count=1`: transpile.go changed; do not trust the cache. Leftover `epmd`/`beam.smp` cause flaky failures — see the `integration-test-leftover-nodes` memory.)

- [ ] **Step 5: Run the full integration suite (ladder + cli) to confirm no regression**

Run:
```bash
pkill -9 -x beam.smp; pkill -9 -x epmd
go test -tags integration -count=1 ./internal/pkg/ladder/ ./internal/pkg/cli/
```
Expected: PASS both.

- [ ] **Step 6: Commit**

```bash
git add testdata/typeswitch/classify_mixed.go internal/pkg/ladder/
git commit -s -m "test: runnable rung — mixed value type switch (primitive + struct + default) compiles and runs"
```

---

## After all tasks

- Run `go test ./...` and both integration suites once more (clean BEAM state first).
- This is a feature step on the 0.3.x line — the release ritual (VERSION bump, squash-merge, **Copilot gate on the release diff**, push origin→upstream→github, GitHub release, `production-0.3.6`, handover/README refresh) follows per `HANDOVER.md` "Release ritual". The Copilot gate is mandatory: it has caught a real silent mis-transpile on every 0.3.x release. For 0.3.6 the gate template is the **disjointness argument** (Task 2, Step 3 comment): confirm the four guards + struct tuples are mutually exclusive so first-match order cannot diverge from Go.

## Self-review notes (author)

- **Spec coverage:** primitive guards (Task 2) ✓; whole-alias V, both primitive-always and struct-conditional (Tasks 1–2) ✓; both forms (Task 2 Step 6 receive test) ✓; `default:` required unchanged (Task 2 Step 7 reject) ✓; `seenTag` primitive dedup (Task 2 Step 7) ✓; disjointness safety documented (Task 2 Step 3, After-all-tasks) ✓; bound-collision reject (Task 1 Step 8) ✓; runnable rung (Task 3) ✓. Deferred items (multi-type, tagless, wider primitives, nil) stay rejected — no task, by design.
- **Type consistency:** `caseTypeName` returns `(name, guard, err)` from Task 2 on; Task 1 still calls the two-value form and Task 2 migrates the caller in the same step it changes the signature (Task 2 Step 3). `emitCaseClause` gains the `guard` param in Task 2 Step 4, and its sole caller is updated in Task 2 Step 3 — same task, consistent. `registerAlias`/`bodyUsesBareAlias` signatures stable across tasks.
- **Ordering:** the `emitExpr` bare-alias change (Task 1 Step 5) lands with the struct whole-alias binding that makes it meaningful, so no intermediate emits an unbound `V`.
