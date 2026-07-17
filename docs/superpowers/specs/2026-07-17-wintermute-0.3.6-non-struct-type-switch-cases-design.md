# Wintermute 0.3.6 — non-struct type-switch cases + whole-alias V

Design doc. Sixth feature step of the 0.3.x transpiler line. Written 2026-07-17.

## Goal

Widen the type switch (both the plain-value form of 0.3.5 and the receive form of
0.3.4) to accept **non-struct cases** over Go's primitive types, lowered to
Erlang **type guards**, and to bind the switch alias `V` to the **whole matched
value** (not only `V.Field`). The natural continuation of the 0.3.4/0.3.5
type-switch work: struct cases dispatch on a message tag, primitive cases
dispatch on a runtime type guard.

```go
switch V := X.(type) {
case int:
    return V + 1        // V is the whole int
case Ping:
    return V.Seq        // struct field (0.3.5); bare V also allowed now
default:
    return 0
}
```

lowers to

```erlang
case X of
    N when is_integer(N) -> N + 1;
    {ping, Seq} -> Seq;
    _ -> 0
end
```

(The Erlang variable is the uppercased alias `V`; `N`/`Seq` above are
illustrative.)

## Scope

**In scope (0.3.6):**

- **Four primitive case types**, each mapping to exactly one Erlang type guard:
  - `int` → `is_integer(V)`
  - `string` → `is_binary(V)` (Go strings are already Erlang binaries `<<"…">>`)
  - `bool` → `is_boolean(V)`
  - `float64` → `is_float(V)`
- **whole-alias `V`**:
  - **Primitive cases**: the alias is always bound (the guard needs the named
    variable — `is_integer(V)`), and the body uses `V` as the whole value.
  - **Struct cases**: the alias binds the whole tuple **in addition** to the
    field access, but only when the body references bare `V` (Erlang alias
    pattern `V = {ping, Seq}`). A field-only struct case stays exactly as in
    0.3.5 (`{ping, Seq}`, no alias binding, no unused-variable warning).
- **Both forms**: non-struct guards fall out of the shared clause builder
  (`emitTypeSwitchClauses`), so they apply to both the value form (`case X of`)
  and the receive form (`switch v := otp.Receive().(type)` → `receive …`).

**Out of scope (loud reject, → 0.3.7+):**

- Multi-type cases (`case Ping, Pong:`) — still rejected (unchanged 0.3.5 error).
- Tagless `switch M.(type)` (no alias) — unchanged reject.
- Further primitive types (`int8/16/32/64`, `uint*`, `float32`, `byte`,
  `[]byte`, `rune`, named types) — rejected as "does not name a struct type"
  becomes "unsupported case type"; enumerated set only.
- `nil` case.

## Design

### 1. Clause forms (the core)

`emitTypeSwitchClauses` (today struct-only) gains a second clause kind. Per case
it distinguishes:

| Case | Erlang pattern | Guard |
|---|---|---|
| Struct `Ping`, field access only | `{ping, Seq}` | — |
| Struct `Ping`, bare `V` used in body | `V = {ping, Seq}` | — |
| Primitive `int`/`string`/`bool`/`float64` | `V` | `when is_integer(V)` / `is_binary(V)` / `is_boolean(V)` / `is_float(V)` |
| `default:` | `_` | — |

A primitive-discriminant table maps the type ident to its guard:

```go
var primitiveGuard = map[string]string{
    "int":     "is_integer",
    "string":  "is_binary",
    "bool":    "is_boolean",
    "float64": "is_float",
}
```

`caseTypeName` routing: first check `em.structs` (struct case), else the
`primitiveGuard` map (primitive case), else loud reject. The two case kinds are
carried into the clause loop so it can emit either a tuple pattern or a guarded
alias variable.

### 2. whole-alias V

The alias `V` (`em.tsAlias`) becomes a real bound Erlang variable:

- **Primitive**: always. The clause pattern is the alias variable itself, with
  the guard: `<Alias> when <guard>(<Alias>) -> Body`.
- **Struct**: only when the body references bare `V` (not as `V.Field`). A new
  helper `bodyUsesBareAlias(body []ast.Stmt, alias string) bool` walks the case
  body with `ast.Inspect`, returning true when it finds an `*ast.Ident` named
  `alias` that is **not** the `.X` selector base of an `*ast.SelectorExpr`
  (`V.Field` does not count as a bare use). When true, the pattern is prefixed
  `V = {ping, Seq}`; otherwise it stays `{ping, Seq}` (unchanged 0.3.5 output,
  no unused-variable warning).
- The alias is registered in the snapshotted `em.bound` scope. If it collides
  with an already-bound name (a parameter, a prior `:=`, or a struct field) it
  is rejected — in Erlang an already-bound pattern variable is an equality
  match, not a fresh binding, so emitting it would silently change the semantics
  (`bound-set-integration` memory). Snapshot/restore around each clause as the
  existing code already does for fields.

Note on the degenerate case: a primitive case whose body never uses `V` still
binds `V` (the guard requires it), producing an `erlc` "variable V unused"
warning — non-fatal, shared with the existing `structPattern` nit (backlog M-A),
and a degenerate case (a type test that discards its value). Documented, not
worked around.

### 3. Safety (the Copilot-gate core)

The four guards and the struct tuples are **pairwise disjoint** predicates:
`is_integer`, `is_binary`, `is_boolean`, `is_float`, and `is_tuple`-structs
(atom-tagged tuples) mutually exclude each other — an atom-tagged tuple is
neither an integer nor a binary nor a boolean nor a float, and the four scalar
guards never overlap (Erlang `is_integer(true)` is false, `is_boolean(1)` is
false, `is_integer`/`is_float` are disjoint — no `is_number` is used). Therefore
Erlang's runtime first-match ordering is **irrelevant**: it coincides with Go's
static per-case type exclusivity. There is **no** fall-through-vs-crash
divergence like the 0.3.5 default-less value switch.

Unchanged safety invariants:

- **`default:` remains required for the value form.** Guards do not change the
  fall-through analysis: a value matching no guard and no struct falls through in
  Go (ordinary control flow) but a total Erlang `case` with no catch-all raises
  `case_clause`. `terminates()` is unchanged (`isReceiveTypeSwitch(s) ||
  hasDefault`). The receive form keeps `default:` optional (a selective receive
  blocks on a non-match, never falls through — including a guarded non-struct
  clause).
- **`seenTag` dedup extends to primitives.** Two `case int:` clauses (the second
  unreachable in Erlang) are rejected, as two struct cases with the same tag
  already are. The discriminant key for a primitive is its guard name (or type
  name); for a struct it stays the lowercased tag.

The spec documents the disjointness argument explicitly as a template for the
Copilot release gate.

### 4. Tests & runnable rung

TDD per unit (red → green first):

- Primitive routing in `caseTypeName` (struct → primitive-map → reject).
- Guard emission for each of the four types (value form).
- `bodyUsesBareAlias` + struct whole-alias pattern (`V = {tag, Fields}` only when
  bare `V` used; field-only stays `{tag, Fields}`).
- Alias `em.bound` collision reject.
- `seenTag` primitive dedup (duplicate `case int:` → reject).
- Receive form with a guarded non-struct clause.

Reject tests:

- Unknown primitive type (e.g. `case int32:`) → "unsupported case type".
- Duplicate `case int:` → unreachable-clause reject.
- Alias colliding with a parameter → bound-collision reject.
- A whole-alias / primitive value switch with no `default:` → default-required
  reject (unchanged 0.3.5 message).

Runnable rung (integration ladder, real `erlc` + `erl`):

- New fixture `testdata/typeswitch/classify_mixed.go` mixing a primitive case, a
  struct case, and a `default:`; transpiles, compiles with `erlc`, and **runs**:
  e.g. `Classify(7) → …`, `Classify({ping,1}) → …`, `Classify(<<"x">>) → …`.
- A receive rung exercising a guarded non-struct clause if it fits the existing
  ladder cleanly.

## Deferred / backlog carried

- Multi-type cases (`case Ping, Pong:`), tagless switch, wider primitive set,
  `nil` case — all still loud-rejected.
- The pre-existing `structPattern` unused-field warning (M-A) and the primitive
  unused-alias warning share the same `_`-prefix fix, still deferred.
- `seenTag` remains per-switch (module-wide lowercased-tag collision M-B unchanged).
