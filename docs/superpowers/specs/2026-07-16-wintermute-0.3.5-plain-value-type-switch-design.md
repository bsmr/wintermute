# Wintermute 0.3.5 — Plain-value type switch (design)

Date: 2026-07-16. Status: approved, ready for planning.

## Summary

0.3.5 is the fifth step of the 0.3.x transpiler-language line. It generalizes the
type switch from a single operand (`otp.Receive()`, added in 0.3.4) to **any
value operand**: `switch v := X.(type)`, lowered to an Erlang **`case X of … end`**.
This is the *other* lowering direction of the same Go syntax — branch on the type
of a value, not on the type of a received message.

```go
switch v := m.(type) {
case Ping:
	otp.Print(v.Data)
case Pong:
	otp.Send(v.To, v.Payload)
default:
	otp.Print("unknown")
}
```
→
```erlang
case M of
    {ping, Data} -> io:format("~p~n", [Data]);
    {pong, To, Payload} -> To ! Payload;
    _ -> io:format("unknown~n", [])
end
```

Scope stays deliberately narrow (the 0.3.1–0.3.4 lesson: thin slices; the Copilot
release gate keeps finding real silent-mis-transpile bugs on the wider surface).
Only struct-typed cases are in — exactly as 0.3.4. Non-struct cases (type guards),
multi-type cases, and passing the whole aliased value are deferred to 0.3.6+.

## Motivating context

The plain-value type switch is structurally almost identical to the 0.3.4 receive
type switch. Three existing facts make this a clean, small slice:

- **The clause machinery already exists.** `emitTypeSwitchReceive` already builds,
  per clause: the struct tuple pattern (`caseTypeName` + `structPattern`), a
  per-clause `em.bound` snapshot/restore, tag-collision rejection, and the optional
  trailing `default:` → `_ ->`. All of this is operand-independent — it works the
  same whether the tuple came from the mailbox or from a value.
- **A struct value *is* a tagged tuple.** In Erlang a struct value carries the same
  shape as a received message — `{ping, Data}` — so the same `structPattern` matches.
- **The operand is just an expression.** Unlike `otp.Receive()`, a plain-value
  operand `X` is emitted through the existing `emitExpr`; no new operand machinery.

The only real difference between the two lowerings is the wrapper: `receive … end`
vs. `case X of … end`. This makes 0.3.5 mostly a **refactor** (extract the shared
clause loop) plus a thin new value path.

## Scope

### In

- `switch v := X.(type)` with one or more `case`-clauses, each naming a single
  struct type declared in the module (`case Ping:` or `case *Ping:` — the star is
  meaningless in Erlang and accepted, as in 0.3.4).
- Operand `X` is any expression `emitExpr` already supports (a parameter, a call
  result, a field access). The valid-Go operand is an `interface{}`/`any`-typed
  parameter (a type switch requires an interface operand in Go); the transpiler
  ignores types and emits `X` verbatim. Per the project-wide rule, the operand
  identifier must be uppercase-leading (an Erlang variable) — e.g. `M any`, not
  `m any` — so `emitExpr` accepts it; a lowercase operand is rejected as for any
  bare ident.
- **Required** `default:` → trailing catch-all `_ ->` (see the revision note under
  "Wrapper and default"; the value form must be total).
- `v.Field` in a clause body → the bound Erlang field variable (`SelectorExpr`
  discards the `v` alias, unchanged from 0.3.4).
- Multiple cases with distinct message tags; struct fields bound per clause in a
  snapshotted `em.bound` scope.

### Out (errors, deferred to 0.3.6+)

- **Non-struct cases** (`case int:`, `case string:`) → Erlang type guards
  (`is_integer`, `is_binary`). Deferred to 0.3.6.
- **Multi-type cases** (`case Ping, Pong:`). Deferred.
- **Whole aliased value** — a bare `v` referring to the entire matched value.
  Still rejected (`em.tsAlias` guard); Erlang would need `V = {tag, …}` binding.
- **Init statement** (`switch n := f(); v := X.(type)`) — still rejected (the init
  would be silently dropped), as in `emitIf`/`emitSwitch`/0.3.4.
- **Non-tail position** — a type switch is only supported as the last statement of
  a tail sequence (unchanged from 0.3.4).

## Lowering rules

### Operand

The operand expression `X` (the `.X` of the `TypeAssertExpr` inside
`ts.Assign`) is emitted via `emitExpr`. It becomes the scrutinee of `case X of`.
Whatever `emitExpr` accepts is accepted; whatever it rejects still errors.

### Wrapper and default

> **Revised during the 0.3.5 release (Copilot gate).** This section originally
> made `default:` optional with let-it-crash on no-match. The release gate proved
> that a silent mis-transpile: a default-less value type switch **falls through**
> in Go when no case matches (ordinary control flow — it proceeds to whatever
> follows), whereas a total Erlang `case` with no catch-all raises `case_clause`.
> Go returns normally where Erlang crashes. The value form therefore **requires a
> default** (a receive, which blocks on a non-match and never falls through, keeps
> it optional). The corrected rules:

- With at least one struct case → `case <X> of\n  <pattern> -> <body>;\n  … \nend`.
- `default:` is **required** for the value form. With → trailing `_ -> <body>`
  (the catch-all makes the `case` total). **Without → rejected** with "a
  plain-value type switch requires a default clause" — the default-less form
  falls through in Go, which a total Erlang `case` cannot express.
- `terminates()` counts a plain-value type switch as terminating **only with a
  default** (`isReceiveTypeSwitch(s) || hasDefault`): a value `case` falls through
  in Go without a default, so a default-less one does not terminate and may not be
  a bare-`if` then-branch. A **receive** type switch still terminates without a
  default (it blocks on a non-match, never falling through).

### Clause body and field binding

Unchanged from 0.3.4: each clause snapshots `em.bound` (`maps.Clone`), binds its
struct fields via `structPattern`, emits the body with `em.tsAlias` set to the
alias name, then restores `em.bound`. `v.Field` → `Field`; bare `v` is rejected.

## Refactor (the core of the change)

Extract the shared clause loop currently inside `emitTypeSwitchReceive`
(transpile.go ≈ 586–637: per-clause parsing, tag-collision map, `structPattern`,
`em.bound` snapshot/restore, `default:` handling) into a helper:

```
emitTypeSwitchClauses(ts) -> (clauses []string, err error)
```

that returns the ordered `pattern -> body` clause strings **including** the
trailing `_ -> deflt` when a default is present. The default handling is identical
in both paths (`haveDefault` → append `_ -> deflt`), so it lives entirely in the
helper.

Two thin wrappers consume it:

- `emitTypeSwitchReceive` → `receive\n <clauses> \nend`.
- `emitTypeSwitchValue` (new) → `case <operand> of\n <clauses> \nend`.

A single entry point dispatches on the operand:

```
emitTypeSwitch(ts):
    set/restore em.tsAlias, reject init statement (shared)
    if isReceiveTypeSwitch(ts): emitTypeSwitchReceive
    else:                       emitTypeSwitchValue
```

`emitStmts` (transpile.go ≈ 351) calls `emitTypeSwitch` instead of
`emitTypeSwitchReceive` directly. The current `"type switch on a plain value is
unsupported (0.3.5+)"` error (≈ 576) is removed — that case is now served.

The `em.tsAlias` set/restore and the init-statement rejection are shared concerns
and move to the `emitTypeSwitch` entry point (or stay duplicated if that reads
cleaner — an implementation detail for the plan).

## Runnable rung

New fixture `testdata/typeswitch/classify.go`: a function with an `any` parameter
that classifies it over two declared struct types via a plain-value type switch
(valid Go — the interface operand makes `.(type)` legal). The operand and alias
are uppercase (Erlang-variable rule).

```go
type Ping struct{ Data int }
type Pong struct{ Data int }

func Classify(M any) { … switch V := M.(type) { case Ping: …; case Pong: …; default: … } }
```

The ladder integration test transpiles it, compiles with `erlc`, and calls the
transpiled function directly from the Erlang side with `{ping, 1}` and `{pong, 2}`
(this sidesteps struct-literal construction, which the subset does not model) —
asserting both branches. The fixture carries a `default:` (required for the value
form), so a non-matching value takes the catch-all rather than crashing.

## Testing (TDD, red → green)

Unit tests in `transpile_test.go`:

- plain-value type switch **with** default → `case X of … ; _ -> … end`.
- plain-value type switch **without** default → **rejected** ("requires a default
  clause"), in tail position and as a bare-`if` then-branch (the fall-through
  regression the release gate caught).
- `v.Field` in a clause body binds to the field variable.
- two cases with distinct tags emit both clauses in order.
- **inherited rejections still fire on the value path:** tag collision
  (`Ping` + `*Ping`), bare-`v` alias, non-struct case, multi-type case, init
  statement, non-tail position.
- the receive path is unchanged (existing 0.3.4 tests stay green — regression
  guard for the refactor).

Plus the `classify.go` integration fixture above.

## Non-goals / deferred (0.3.6+)

Non-struct cases → guards (the natural next step: `case int:` →
`when is_integer(N)`), multi-type cases, whole-alias `v`, `after`/`fallthrough`,
init statements. Framing unchanged: the transpiler covers only what maps cleanly
to Erlang; loops, comprehensions, and mutable state stay in the native-`.erl`
escape hatch (0.2.7), not the transpiler.
