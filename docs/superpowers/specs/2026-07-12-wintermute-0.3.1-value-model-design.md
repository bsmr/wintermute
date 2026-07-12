# Wintermute 0.3.1 — Value Model (design)

Date: 2026-07-12. Status: approved, ready for planning.

## Summary

0.3.1 opens the 0.3.x transpiler-language line. It is the **value-model** step:
Go functions gain **parameters**, a **trailing `return`** value, **local
bindings** (`:=`), and **calls with arguments** (self and sibling). This is the
load-bearing architectural shift of the 0.3.x line — from the current flat
"comma-sequence of expression statements" body to a body that binds names and
yields a value — but it deliberately stops short of control flow. `if`/`case`/
`switch`, comparison/boolean operators, and guards are 0.3.2+.

Framing (from the 0.3.0 coverage analysis, carried into the handover): the
transpiler covers only what maps cleanly to Erlang. 0.3.1 keeps that contract —
every construct here has a one-to-one Erlang mapping and no hidden automatism.

## Scope

### In scope

- **Function parameters.** `func Add(X, Y int) int` → `add(X, Y) ->`. Parameter
  names must be uppercase-leading (Erlang variables), rejected otherwise —
  consistent with the existing struct-field guard. Go types are ignored (Go-only,
  for tooling), consistent with the existing `TypeAssertExpr` handling.
- **Trailing `return`.** `return expr` is honoured **only as the last statement**
  of a function body, emitting `expr` as the body's final (return) value. A
  function with no `return` keeps today's behaviour: the last expression is the
  value; an empty body emits `ok`.
- **Local bindings.** `Z := expr` → Erlang match `Z = expr`. The bound name must
  be uppercase-leading. Re-assignment (`z = ...`, `*ast.AssignStmt` with `token.ASSIGN`)
  is rejected — Erlang variables are immutable.
- **Calls with arguments.** `f(A, B)` (bare-ident calls to same-module top-level
  functions, including self-calls) → `f(A, B)`. Arguments are any emittable
  expression. The current nullary-only guard on bare-ident calls is removed.
- **Export arity.** Exported functions export as `f/N` (N = parameter count),
  replacing the hardcoded `f/0`.

### Recursion — honest boundary

The **mechanism** is enabled: a self-call with arguments emits correctly, and
Erlang performs last-call optimization for free, so a tail-recursive call costs no
stack. But **useful** recursion needs a base-case branch — that requires
`if`/`case`, which is 0.3.2. 0.3.1 only unblocks emitting the recursive call; it
does not claim runnable recursive algorithms.

### Explicitly deferred (0.3.2+)

- Early / multiple return points (needs control flow to restructure into Erlang).
- Multiple return values → Erlang tuple.
- Operators beyond `+`: comparison (`==` `!=` `<` `>` `<=` `>=`), boolean
  (`&&` `||` `!`).
- `if` / `case` / `switch` → Erlang `case`.
- Guards (open question: no clean Go→Erlang-guard mapping yet; unresolved, does
  not block 0.3.1).
- Cross-module plain calls (`pkg.Func(...)`); only `otp.*` markers and
  same-module functions are callable.

## Architecture

**Approach A — minimal statement-set extension (chosen).** The existing
`emitStmts` flat comma-sequence model already yields "last expression = value".
0.3.1 adds exactly:

1. Two `emitStmt` cases: `*ast.AssignStmt` (`:=` → `Var = expr`) and
   `*ast.ReturnStmt` (last-position only → its expression as the trailing value).
2. Argument emission in `emitCall`'s bare-ident branch (remove the nullary guard).
3. Correct export arity in `Module`.
4. A `bound map[string]bool` on `emitter` (seeded with the function's parameter
   names) to reject rebinding a name — otherwise a duplicate `X = ...` would emit
   valid-looking Erlang that fails at runtime with `badmatch`.

Rejected — **Approach B**, building a "block yields a value" tree abstraction now
to anticipate 0.3.2's `if`/`case`. YAGNI: 0.3.2 refactors when it actually needs
the tree. The flat model is correct for the 0.3.1 surface.

### Emitter changes (map)

- `emitter`: add `bound map[string]bool` (reset per function).
- `Module`: bind parameter names into `bound`; drop the "parameters are not yet
  supported" rejection (lines ~105–107); export arity `f/N`.
- `emitStmt`: add `*ast.AssignStmt` and `*ast.ReturnStmt` cases.
- `emitCall`: bare-ident branch emits arguments; drop the nullary-only guard
  (lines ~440–442).

Parameter references in bodies already work: the `*ast.Ident` case emits an
uppercase-leading name as-is (and rejects lowercase), and `*ast.SelectorExpr`
handles `param.Field`.

## Error handling

All errors are source-positioned via the existing `em.errorf`, matching the
current message style.

| Condition | Message (intent) |
|---|---|
| `return` not the last statement | early return needs `case`/`if` (0.3.2) |
| re-assignment (`x = ...`) | Erlang variables are immutable; single-assignment only |
| rebinding a bound name (param or prior `:=`) | name already bound; Erlang has no rebinding |
| lowercase-leading parameter | Erlang variables must be uppercase |
| lowercase-leading `:=` target | Erlang variables must be uppercase |

Deferred constructs (multi-return, operators other than `+`, `if`/`case`) keep
erroring as they do today, with messages pointing at the 0.3.2 roadmap where
useful.

## Testing

TDD, red → green, tests before implementation (project rule).

- **Unit tests** (`transpile_test.go`) for each in-scope construct and each error
  path: parameters + arity, trailing `return`, local binding, `:=` immutability
  rejection, rebinding rejection, lowercase rejections, self/sibling call with
  args.
- **Real-toolchain check** (per the `run-real-toolchain-build-early` memory —
  green units are not enough): one `testdata/` fixture exercising parameters +
  bindings + a call-with-args that transpiles **and compiles with `erlc`**, wired
  as a rung in the integration tests (`-tags integration`). Green units alone do
  not prove the emitted Erlang is valid.

## Non-goals / constraints

- Stdlib only, no third-party modules (project rule).
- `main()` → `run()` untouched; this is transpiler-internal.
- No `pkg/otp` marker changes → SDK index unchanged.
- Deterministic output preserved (stable ordering already enforced in `Module`).

## Key artifacts

- Predecessor: `docs/superpowers/specs/2026-07-12-wintermute-0.3.0-promotion-design.md`
- Transpiler: `internal/pkg/transpile/transpile.go` (+ `_test.go`)
- Roadmap context: `HANDOVER.md` ("Next step: 0.3.x transpiler-language work")
