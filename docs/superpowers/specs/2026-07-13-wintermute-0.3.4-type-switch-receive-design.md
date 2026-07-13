# Wintermute 0.3.4 — Type-switch receive (design)

Date: 2026-07-13. Status: approved, ready for planning.

## Summary

0.3.4 is the fourth step of the 0.3.x transpiler-language line. It adds exactly
one new form: the **type switch over a received message**,
`switch v := otp.Receive().(type)`, lowered to a **multi-clause Erlang
`receive`**. This is the idiomatic Erlang process dispatch — a process waits for
a message and branches on its type — and it generalizes the single-clause
receive from 0.3.1 (`X := otp.Receive().(T)`) to N message types.

```go
switch v := otp.Receive().(type) {
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
receive
    {ping, Data} -> io:format("~p~n", [Data]);
    {pong, To, Payload} -> To ! Payload;
    _ -> io:format("unknown~n", [])
end
```

Scope is deliberately narrow (the 0.3.1–0.3.3 lesson: thin slices; the Copilot
release gate keeps finding real silent-mis-transpile bugs in the wider surface).
Only struct-typed cases over an `otp.Receive()` operand are in; everything else
errors and is deferred to 0.3.5+.

## Motivating context

Two existing facts make this a clean, small slice:

- **`any` is free.** The transpiler ignores parameter *types* entirely (only
  names matter — Erlang is dynamically typed), so the operand of a type switch
  needs no new `interface{}`/`any` machinery.
- **`x.(T)` already exists.** The 0.3.1 single-clause receive lowers
  `X := otp.Receive().(Ping)` to the tuple pattern `{ping, Field1, …}` (tag =
  lowercased type name, each declared field bound to its capitalized Erlang
  variable) and registers each field in `em.bound`. The type switch reuses this
  tuple-pattern builder per clause.
- **Field access is already modelled.** `v.Data` lowers to the bound variable
  `Data` (the `SelectorExpr` case discards the `v` alias — field names *are* the
  module-wide Erlang variables). So a type-switch clause body uses its struct's
  fields exactly as a single-clause receive body does.

## Scope

### In

- `switch v := otp.Receive().(type)` with one or more `case`-clauses, each naming
  a single struct type declared in the module.
- Lowering to a multi-clause `receive … end`, terminal and in tail position.
- Per-clause tuple pattern `{tag, Field…}`, reusing the 0.3.1 receive builder.
- Field access in a clause body via `v.Field` → `Field`.
- **Optional `default`:**
  - **with** `default:` → a trailing catch-all clause `_ -> …` (matches any other
    message immediately).
  - **without** `default:` → selective receive; non-matching messages stay in the
    mailbox and the process blocks until a listed type arrives. No catch-all is
    emitted.
- Per-clause `em.bound` snapshot/restore (the `emitBranch` pattern), so sibling
  clauses may rebind the same field names freshly; a field-name collision with an
  **outer** binding (a parameter, a prior `:=`) is rejected — the fourth
  `bound-set-integration` context.

### Out (deferred → 0.3.5+, all error, no silent mis-transpile)

- Type switch on a **plain value** (`switch v := X.(type)` where `X` is not
  `otp.Receive()`) → would lower to a `case`, a distinct target form.
- **Non-struct** case types (`case int`, `case string`, …) → Erlang type guards,
  a different mechanism.
- **Multi-type** cases (`case Ping, Pong:`) — the field set is not uniquely
  bindable.
- Passing the **whole** aliased value (bare `v`, not `v.Field`).
- `after`-timeout, `fallthrough`, an init statement, tagless type switch.

The 0.3.1 single-clause `X := otp.Receive().(T)` form is unchanged and coexists.

## Approach

A dedicated emit path, chosen over generalizing the existing receive or folding
into `emitSwitch`:

- `emitStmt` recognizes the pattern: an `*ast.TypeSwitchStmt` whose `Assign` is
  `v := <expr>.(type)` and whose `<expr>` is exactly `otp.Receive()`. It calls a
  new `emitTypeSwitchReceive`.
- `emitTypeSwitchReceive` emits `receive` + one clause per `case`, reusing the
  0.3.1 tuple-pattern builder for each clause's `{tag, Field…}` and `emitBranch`
  for each clause body's scoped lowering. A `default:` becomes the trailing
  `_ -> …`.
- Any other `*ast.TypeSwitchStmt` shape (plain-value operand, tagless, init) is
  rejected in `emitStmt` with a positioned "unsupported (0.3.5+)" error.

Rationale: a type-switch-receive is mechanically a *different* construct
(`receive`, not `case`), so it earns its own clearly named path that shares only
the pattern builder — DRY where it matters, no entanglement with tested 0.3.1/
0.3.3 code.

## Error handling (rejects)

All out-of-subset input is rejected loudly with a positioned error:

| Input | Behaviour |
|---|---|
| `switch v := otp.Receive().(type)` with struct cases (+ optional default) | ✅ emits `receive … end` |
| operand ≠ `otp.Receive()` (a parameter/variable) | ❌ "type switch on a plain value is unsupported (0.3.5+); the operand must be otp.Receive()" |
| `case` names a non-struct type (`int`, `string`, …) | ❌ "type switch case must name a struct type" |
| `case Ping, Pong:` (multi-type) | ❌ "multi-type case is unsupported (0.3.5+)" |
| field name collides with an outer binding | ❌ existing collision message (rename one) |
| empty clause body | ❌ "case clause has no value (empty body)" |
| bare `v` used in a body (not `v.Field`) | ❌ "the type-switch alias must be used via field access (v.Field); passing the whole value is unsupported (0.3.5+)" |
| type switch not terminal / not in tail position | ❌ as `emitSwitch` |
| `case` names an unknown struct type (not declared in the module) | ❌ "unknown struct type in case" |

### `terminates()` integration

A type-switch-receive **terminates once every clause terminates** — the
`default` is *not* a precondition for termination here. This differs from the
`case`-`switch` (0.3.3), where "terminating" requires a `default` because a
non-matching value would fall through. A `receive` cannot fall through: it only
proceeds on a match and then yields exactly that clause's value. So an exhaustive
default is unnecessary for termination; a selective (no-default) receive still
terminates. This distinction is documented as a comment in `terminates()`.

## Testing

TDD, red→green per unit.

- **Unit tests (`transpile_test.go`)** — one per semantic and reject rule:
  - happy path: two struct cases + default → correct `receive … {_ -> …} end`;
    field binding (`v.Data` → `Data`) in the body.
  - selective receive: two cases **without** default → `receive` with no
    catch-all.
  - `case *Ping` equals `case Ping` (pointer star ignored).
  - field collision with an outer binding → reject (`bound-set-integration`).
  - each reject from the table above → an assertion on the error message.
  - `terminates()`: a type-switch-receive as the then-branch of a bare `if`
    accepted, both with and without default.
- **Ladder integration (`ladder_integration_test.go`)** — a fixture
  `testdata/typeswitch/dispatch.go`: a process receives two message types,
  branches, and replies. Transpiles → compiles with `erlc` → **runs** and yields
  the expected result for each sent message type (the real multi-clause receive
  dispatch closes).
- **Security sweep** unchanged (govulncheck / gitleaks / gosec baseline).

Pre-merge verification: `go test ./...`; both integration suites `-count=1`
green; `pgrep beam.smp` = 0; then the gated-release ritual including the Copilot
gate on the release diff.

## Non-goals

No new value types, no `interface{}` surface, no timeout/`after`, no change to
the 0.3.1 single-clause receive or the 0.3.3 tagged switch. This slice is only
the multi-clause receive dispatch.
