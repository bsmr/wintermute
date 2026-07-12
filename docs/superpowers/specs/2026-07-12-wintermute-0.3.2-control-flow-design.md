# Wintermute 0.3.2 — Control Flow (design)

Date: 2026-07-12. Status: approved, ready for planning.

## Summary

0.3.2 is the second step of the 0.3.x transpiler-language line. It adds
**operators** (arithmetic beyond `+`, comparison, boolean) and **`if` → Erlang
`case`**. This is where 0.3.1's flat "comma-sequence" function body finally
becomes a **value-yielding tree**: an `if` in tail position emits a `case`
expression whose branches each yield a value. Together, operators + `if` make
0.3.1's recursion *useful* — a base-case branch becomes expressible:

```go
func Fact(N int) int {
	if N == 0 { return 1 }
	return N * Fact(N-1)
}
```
→
```erlang
fact(N) -> case N =:= 0 of true -> 1; false -> N * fact(N - 1) end.
```

Scope is deliberately narrow (the 0.3.1 lesson: thin slices, and complexity
hides cross-cutting bugs). `switch`, `else if` chains, and side-effect-only
`if` are 0.3.3+.

## Scope

### In scope

**Operators.** `emitExpr` handles a full binary-operator table plus unary `!`:

| Go | Erlang | Notes |
|---|---|---|
| `+` `-` `*` | `+` `-` `*` | `+` already present |
| `/` | `div` | Go int `/` is integer division |
| `%` | `rem` | |
| `==` `!=` | `=:=` `=/=` | exact term equality (faithful Go `==`, no coercion) |
| `<` `>` `>=` | `<` `>` `>=` | |
| `<=` | `=<` | Erlang spells it `=<` |
| `&&` `\|\|` | `andalso` `orelse` | short-circuit |
| `!` (unary) | `not ` | new `*ast.UnaryExpr` case |

**Precedence safety.** A binary operand that is itself a `*ast.BinaryExpr` is
parenthesized: `(a+b)*c` → `(a + b) * c`. A single operator stays bare
(`X + Y` unchanged — no regression on existing `+`-asserting tests). No
precedence table; parenthesizing nested binaries is always correct.

**`if` → `case`.** Handled in `emitStmts`, **tail position only**. An `if`
consumes the rest of the statement list (everything after it is the
continuation), so an `if` always extends to the end of its sequence. Two shapes:

- **if/else** — both branches end in `return`:
  ```go
  if Cond { return A } else { return B }
  ```
  → `case <Cond> of true -> <A>; false -> <B> end`. The if/else must be the last
  statement; any statement after it is unreachable → error.
- **bare `if` + continuation** (the base case) — then-block ends in `return`,
  statements after the `if` form the false-branch:
  ```go
  if Cond { return A }
  <continuation, ending in a value>
  ```
  → `case <Cond> of true -> <A>; false -> <continuation> end`.

**Branch scoping.** Erlang `case` clauses are independent scopes: `Z := ...` in
the then-branch and `Z := ...` in the else/continuation branch are each a fresh
bind. The flat `bound` set would wrongly reject the second as "already bound".
Fix: **snapshot `em.bound` (`maps.Clone`, stdlib) before each branch and restore
it after.** A collision with an *outer* name (a parameter or a `:=` bound before
the `if`) is still rejected, because the snapshot contains those names; sibling
branches may reuse a name freshly. This is the `bound-set-integration` invariant
applied to a new binding context — the exact class of bug the Copilot gate caught
in 0.3.1, so it is tested explicitly (sibling reuse AND outer collision).

### Explicitly deferred (0.3.3+)

- `switch` / `case`-on-value.
- `else if` chains (nested `case`) — rejected with a positioned error.
- side-effect-only `if` (a branch that does not `return`) — rejected.
- multiple return values → tuple; guards; cross-module plain calls.

## Architecture

Extends the existing emitter (`internal/pkg/transpile/transpile.go`), no rewrite.

- **`emitExpr` — `*ast.BinaryExpr`:** replace the `+`-only branch with a lookup
  in a `binOp map[token.Token]string`. Emit each operand via `emitExpr`,
  parenthesizing an operand that is itself a `*ast.BinaryExpr`. An unmapped
  operator errors (positioned), pointing at the 0.3.3 roadmap where useful.
- **`emitExpr` — new `*ast.UnaryExpr`:** `token.NOT` → `not <X>` (parenthesize a
  binary operand). Other unary ops error.
- **`emitStmts` — new `*ast.IfStmt` handling:** when a statement is an `*ast.IfStmt`:
  - Require `isTail` (else error: control flow only in tail position).
  - Reject `else if` (`ifStmt.Else` is an `*ast.IfStmt`) → positioned error.
  - Emit `Cond` via `emitExpr`.
  - Emit the then-block as a value (snapshot/restore `bound`); require it to end
    in `return` (or otherwise yield) — a non-terminating then-block errors.
  - False-branch value:
    - if `ifStmt.Else` is a `*ast.BlockStmt`: emit it (snapshot/restore); require
      it to terminate; the `if` must be the last statement (statements after →
      unreachable error).
    - else (no else): the continuation `list[i+1:]` is the false-branch (snapshot/
      restore); a missing continuation → error (false-branch has no value).
  - Emit `case <Cond> of true -> <then> ; false -> <else> end` as the sequence's
    trailing value.
- A small helper emits a block/continuation as a value-yielding Erlang sequence,
  reusing the `emitStmts(list, true)` return-position logic.

The `bound` snapshot/restore is the load-bearing correctness mechanism; it is
implemented as save `snap := maps.Clone(em.bound)` / `em.bound = snap` around each
branch emission.

## Error handling

All errors positioned via `em.errorf`.

| Condition | Message (intent) |
|---|---|
| unmapped binary operator | operator X is unsupported (0.3.3) |
| unary op other than `!` | unary operator X is unsupported |
| `if` when `!isTail` | control flow is only supported in tail position |
| `else if` chain | else-if chains are unsupported (0.3.3); use a nested if — rejected |
| `if` with an init clause (`if x := …; cond`) | unsupported (0.3.3+) |
| **bare `if` whose then-branch does not terminate** | then-branch must end in a return; otherwise its fall-through to the continuation cannot be a terminal Erlang case clause |
| if/else followed by more statements | unreachable statement after a terminating if/else |
| bare `if` with no continuation | a bare if needs a following value (the false branch) |
| `:=` colliding with an outer bound name inside a branch | (existing) already bound |

A branch's value is its last statement's value (a `return`'s expression, or the
last expression — consistent with the 0.3.1 body model), so if/else branches are
**not** required to end in `return`. The termination rule is enforced only for a
**bare `if`'s then-branch**, because there — and only there — a non-returning
branch would fall through to the continuation in Go, which a terminal Erlang
`case` clause cannot express (it would silently yield the branch value instead).
A `terminates()` helper recognizes a `return` or an if/else whose both branches
terminate.

## Testing

TDD, red → green, tests before implementation.

- **Unit tests** (`transpile_test.go`): each operator + its Erlang mapping;
  precedence parenthesization (`(a+b)*c`, and `X+Y` staying bare); unary `!`;
  if/else; bare-if base case; each error path (unmapped op, else-if, non-terminating
  branch, unreachable-after-if/else, bare-if-no-continuation, if in non-tail).
- **Branch-scoping tests:** (a) sibling reuse — `Z :=` in both then and else
  branches is accepted; (b) outer collision — `Z :=` in a branch when `Z` is a
  parameter or bound before the `if` is rejected.
- **Real-toolchain rung** (`-tags integration`, per `run-real-toolchain-build-early`):
  a **runnable recursive function** (factorial) fixture that transpiles, compiles
  with `erlc`, AND executes with a checked result (e.g. `fact(5) = 120`) — proving
  the base-case loop actually closes, not just that it compiles.

## Non-goals / constraints

- Stdlib only (`maps.Clone` is stdlib, Go 1.21+).
- No `pkg/otp` marker changes → SDK index unchanged.
- Deterministic output preserved.
- `main()` → `run()` untouched; transpiler-internal.

## Key artifacts

- Predecessor: `docs/superpowers/specs/2026-07-12-wintermute-0.3.1-value-model-design.md`
- Transpiler: `internal/pkg/transpile/transpile.go` (+ `_test.go`)
- Invariant: the `bound-set-integration` memory (new binding contexts must scope correctly)
- Roadmap context: `HANDOVER.md` ("Next step: 0.3.2 — operators + if/case/switch")
