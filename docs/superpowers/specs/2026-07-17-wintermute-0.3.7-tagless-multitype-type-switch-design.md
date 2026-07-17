# Wintermute 0.3.7 — tagless + multi-type type switch (+ nested-alias fold)

Design doc. Seventh feature step of the 0.3.x transpiler line. Written 2026-07-17.

## Goal

Close the type-switch story by adding the two remaining clean forms:

- **Tagless form** `switch M.(type)` — a type switch with no alias binding
  (detection-only cases).
- **Multi-type cases** `case Ping, Pong:` — one Go clause matching several types,
  expanded to several Erlang clauses sharing a body.

Plus a small correctness **fold**: scope `bodyUsesBareAlias` so a nested type
switch reusing the enclosing alias name no longer triggers a false collision
rejection (the 0.3.6 Copilot-gate MINOR).

Explicitly **out of scope** and still loud-rejected (see Non-goals): wider
primitive types (`int32`/`float32`/`byte`/`[]byte`/named types) — they collapse
onto the same Erlang guard as `int`/`float64`/`string` (Erlang has no fixed-width
integers), so admitting them would make two statically-distinct Go cases share
one guard: a silent mis-transpile. They stay rejected until a dedicated
collision-aware design.

```go
// Tagless — no alias; cases are detection-only (no value/field reference).
switch M.(type) {
case Ping:
    log()
default:
    noop()
}

// Multi-type — one clause, several types; bare V (whole value) is legal, V.Field is not.
switch V := M.(type) {
case Ping, Pong:
    handle(V)
case int:
    log()
default:
    noop()
}
```

## Scope

**In scope (0.3.7):**

- **Tagless form** `switch M.(type)` (no alias), in both the value form
  (`case M of …`) and the receive form (`receive …`). Cases are detection-only —
  the body can reference neither the matched value nor its fields (there is no
  variable). A struct case matches the tag with wildcard fields
  (`{ping, _}`); a primitive case guards a fresh throwaway variable
  (`_V when is_integer(_V)`).
- **Multi-type cases** `case Ping, Pong:` — one Go clause expands to N Erlang
  clauses (one per listed type) sharing the (duplicated) body. Fields are never
  bound (Go keeps `v` at the interface type in a multi-type case, so `v.Field` is
  invalid). The alias binds the whole value (`V = {ping, _}` / `V when
  is_integer(V)`) when the body uses bare `V`, else wildcards / a throwaway var.
- **The nested-alias fold** for `bodyUsesBareAlias`.

**Non-goals (loud reject, → 0.3.8+):**

- Wider primitive types (`int8/16/32/64`, `uint*`, `float32`, `byte`, `rune`,
  `[]byte`, named types) — guard-collision hazard, see Goal.
- `nil` cases.
- Field access inside a multi-type case (`case Ping, Pong: use(v.Field)`) — Go
  forbids it; the transpiler rejects it loudly (see Design §3).
- gen_server callback completion (`handle_cast`/`handle_info`/`terminate`/
  `code_change`) / `gen_statem` — a separate subsystem, its own future cycle.

## Design

### 1. Tagless form

In go/ast, a tagless `switch M.(type)` has `ts.Assign` of type `*ast.ExprStmt`
(wrapping the `*ast.TypeAssertExpr`), whereas an aliased `switch v := M.(type)`
has `ts.Assign` of type `*ast.AssignStmt`. `emitTypeSwitch` today rejects the
non-`AssignStmt` case ("type switch must bind an alias"). New behaviour:

- Recognise the `*ast.ExprStmt` form, extract the operand from the
  `TypeAssertExpr`, set `em.tsAlias = ""` (no alias), and dispatch to the value
  or receive lowering exactly as the aliased form does (based on whether the
  operand is `otp.Receive()`).
- With `em.tsAlias == ""`, no clause binds an alias:
  - **Struct case** `case Ping:` → `{ping, _}` — the tag plus one `_` per
    declared field (arity from `em.structs`), via a new `structWildcardPattern`
    helper that emits wildcards and does **not** touch `em.bound`.
  - **Primitive case** `case int:` → `_V when is_integer(_V)` — a fresh
    underscore-prefixed throwaway variable (a bare `_` is illegal in a guard;
    the `_`-prefix suppresses the "unused variable" warning since the body never
    references it). The same reserved name `_V` is reused across clauses; each
    Erlang clause is its own scope, so there is no conflict.
  - `default:` → `_ -> Body` (unchanged).
- **`default:` stays REQUIRED for the value form.** A tagless value switch whose
  operand matches no case falls through in Go, so a total Erlang `case` would
  raise `case_clause` — the same rule and shared `haveDefault` check as 0.3.6.
- `isReceiveTypeSwitch` is generalised to extract the operand from **either**
  `ts.Assign` shape (`AssignStmt` or `ExprStmt`), so a tagless receive switch is
  correctly recognised (otherwise `terminates()` would treat a blocking tagless
  receive as non-terminating).

### 2. Multi-type cases

`emitTypeSwitchClauses` today rejects `len(cc.List) != 1`. New behaviour: iterate
the list. In Go, a multi-type case keeps `v` at the interface type, so `v.Field`
is invalid — only bare `v` (the whole value) or no reference is legal.

- One Go clause expands to **N Erlang clauses**, one per listed type, each sharing
  the same body. The body string is emitted once and reused (duplicated) across
  the N clauses — Erlang has no "several patterns, one body" that spans a tuple
  pattern and an integer guard.
- Fields are **never** bound (Go forbids field access here). Each type's pattern
  uses wildcard fields for structs (`structWildcardPattern`) and the guard for
  primitives.
- Whole-alias `V`: computed **once** per Go clause via `bodyUsesBareAlias`. If the
  body uses bare `V`, each expanded clause binds it — `V = {ping, _}` for a struct
  type, `V when is_integer(V)` for a primitive — and the alias is registered once
  in the snapshotted `em.bound` (via the existing `registerAlias`). If the body
  does not use bare `V`, patterns wildcard / use a throwaway (`{ping, _}` /
  `_V when …`). This reuses the 0.3.6 whole-alias machinery directly.
- `seenTag` dedup iterates the list: every listed type is checked against all
  types seen so far (a duplicate within one list **or** across cases → reject, the
  existing "unreachable clause" error).
- **0.3.6 single-struct field-binding cases are unchanged.** Only `len(cc.List) >
  1` takes the wildcard/expansion path. A single struct case (`case Ping:` with
  `v.Seq`) still binds fields via `structPattern` exactly as in 0.3.6.

### 3. Multi-type field-access reject (real safety)

Field access inside a multi-type case must be rejected **loudly**, not emitted.
`emitExpr` lowers `v.Field` to the bare Erlang field name; in a multi-type clause
that field is never bound (wildcarded), so `v.Seq` would either emit an unbound
`Seq` (an `erlc` error — loud) **or**, if some outer scope happens to bind `Seq`,
silently reference that unrelated binding — a silent mis-transpile. So, when
emitting a multi-type clause, walk the case body for a `*ast.SelectorExpr` whose
base is the alias and reject: "field access is not allowed in a multi-type case;
v keeps the interface type". Detection mirrors `bodyUsesBareAlias`'s selector
handling.

### 4. The nested-alias fold

`bodyUsesBareAlias` walks the whole case body including nested statements, so a
bare `V` inside a nested `switch V := … .(type)` (valid Go shadowing) is
misattributed to the outer alias, causing a false "collides with an
already-bound name" rejection (the 0.3.6 gate MINOR — loud, not silent, but a
false rejection of valid code).

Fix: while walking, do **not** descend into a nested `*ast.TypeSwitchStmt` whose
bound alias equals the alias being searched for (the inner shadows the outer — a
bare `V` there resolves to the inner alias, matching how `emitExpr` uses the
innermost `em.tsAlias`). A nested switch binding a different alias (or tagless)
is still descended into (a bare `V` there is the outer's). Implement by pruning
that subtree (`ast.Inspect` returning false) in both passes of the skip-set walk.

### 5. Safety (the Copilot-gate template for 0.3.7)

- **Disjointness unchanged**: the four guards and atom-tagged struct tuples stay
  pairwise disjoint, so Erlang first-match order coincides with Go static
  exclusivity. Multi-type expansion adds clauses but each expanded clause carries
  a disjoint discriminant; body duplication is behaviour-preserving.
- **`default:` required for the value form** holds for tagless and multi-type
  alike (shared `haveDefault` check); the receive form stays default-optional (a
  selective receive blocks, never falls through — including a tagless or
  wildcard-struct clause).
- **The default catch-all is always emitted last** regardless of Go source order
  (unchanged accumulation).
- **Multi-type field access is rejected** (§3) rather than emitted — the one new
  silent-mis-transpile vector this release introduces, closed by design.
- **`em.bound` integration**: the multi-type alias is registered once per Go
  clause; wildcard patterns and throwaway guard vars bind nothing into `em.bound`.
  Snapshot/restore per clause unchanged.

## Testing

TDD per unit (red → green):

- Tagless entry (`ExprStmt` operand) recognised; struct case → `{ping, _}`;
  primitive case → `_V when is_integer(_V)`; a tagless value switch with no
  `default:` → default-required reject; tagless receive form → `receive {ping, _}
  -> …` and a guarded tagless clause.
- Multi-type expansion: struct+struct (`case Ping, Pong:` → two `{…, _}`
  clauses sharing the body), struct+primitive, primitive+primitive; whole-alias
  `V` in a multi-type body → each expanded clause binds `V`; a multi-type body
  using `v.Field` → loud reject; duplicate type within a list and across cases →
  dedup reject.
- The fold: a nested `switch V := … .(type)` inside an outer clause whose alias
  is also `V` → no false collision rejection; a nested switch binding a different
  alias still sees the outer `V` as bare.

Runnable rung (integration ladder, real `erlc` + `erl`):

- A fixture exercising a tagless switch and/or a multi-type case that transpiles,
  compiles with `erlc`, and **runs** to a checked result (e.g. a multi-type
  `case Ping, Pong:` returning a constant for either tag, plus a primitive arm
  and a default). Placed in its own subdir under `testdata/typeswitch/` to keep
  one package per directory.

## Deferred / backlog carried

- Wider primitive types, `nil` cases, gen_server callback completion — all still
  loud-rejected / future cycles.
- The 0.3.6 review MINORs (double `ToLower`, default-block/`emitCaseClause`
  duplication, missing explicit default-collision test) remain deferred.
- The stale `(0.3.6+)` version suffix on the tagless/init reject messages is
  resolved for tagless by this release (tagless becomes supported); the init
  reject message should be aligned to `(0.3.8+)` when touched.
