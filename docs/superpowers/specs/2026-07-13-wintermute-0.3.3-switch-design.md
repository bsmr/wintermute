# Wintermute 0.3.3 — Switch (design)

Date: 2026-07-13. Status: approved, ready for planning.

## Summary

0.3.3 is the third step of the 0.3.x transpiler-language line. It adds the
**tagged expression `switch`** → Erlang `case`-on-value, reusing the 0.3.2
`case`/branch-scoping machinery. Go's `switch` has no implicit fall-through
(each case breaks), which maps 1:1 onto Erlang's independent `case` clauses.

```go
func Classify(N int) string {
	switch N {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "many"
	}
}
```
→
```erlang
classify(N) ->
    case N of
        1 -> <<"one">>;
        2 -> <<"two">>;
        _ -> <<"many">>
    end.
```

Scope is deliberately narrow (the 0.3.1/0.3.2 lesson: thin slices; the Copilot
gate keeps finding real silent-mis-transpile bugs in the wider surface). Only the
tagged expression switch with single literal case values and a required `default`
is in; everything else errors and is deferred.

## Scope

### In scope

- **Tagged expression `switch`** — `switch Tag { case V: … ; default: … }` →
  `case <Tag> of <V> -> <clause>; … ; _ -> <default> end`. The tag is emitted
  once via `emitExpr` (a parameter, a `M.X` receive field, etc.).
- **Case values: basic literals only** (int / string). `case 0:` → `0 ->`,
  `case "hi":` → `<<"hi">> ->`. A non-literal case value (identifier, expression)
  is rejected — an identifier would emit as an Erlang variable pattern that
  matches anything and binds, a silent trap.
- **`default` is required** and emitted as the Erlang catch-all `_ ->`. It is
  **sorted to the end** regardless of its position in the Go source (Erlang
  requires the catch-all last; Go allows `default` anywhere). Exactly one
  `default`.
- **Each clause is emitted in its own binding scope** (`emitBranch`, the 0.3.2
  snapshot/restore of `em.bound`): sibling clauses may reuse a name freshly, a
  collision with an outer binding is rejected. Case literals bind no names, so
  only clause-body `:=` bindings scope — the `bound-set-integration` invariant,
  lighter here than for receive patterns but still exercised.
- **The `switch` is terminal**: no statements may follow it (unreachable → error),
  and it is only valid in tail position.

### Explicitly deferred (0.3.4+)

Each rejected with a positioned error, pointing at the roadmap where useful:

- `switch` without `default` (the no-match case would become the continuation,
  the bare-`if` continuation model — deferred).
- multi-value cases (`case 1, 2:`) → would need an Erlang guard
  (`X when X =:= 1 orelse X =:= 2 ->`).
- tagless `switch { case cond: … }` → an if/`case true`-chain.
- type switch (`switch v := x.(type)`) → Erlang type guards (`is_integer`, …);
  note this DOES bind a name (`v`), a full `bound-set-integration` context.
- `switch` init clause (`switch x := f(); Tag`).
- `fallthrough`.
- non-literal case values; negative-literal case values (`case -1:`).

## Architecture

Extends the existing emitter (`internal/pkg/transpile/transpile.go`), mirroring
`emitIf`.

- **`emitStmts` — new `*ast.SwitchStmt` handling:** like the `*ast.IfStmt` branch,
  at the top of the loop. Require `isTail` (else error). Emit the case. Require no
  statements after the switch (`list[i+1:]` empty, else unreachable error). Append
  and return (the switch is the sequence's value).
- **`emitSwitch(sw *ast.SwitchStmt) (string, error)`** — new method (a type
  switch is a distinct `*ast.TypeSwitchStmt` node and never reaches here; it is
  rejected in `emitStmt`, see below):
  - Reject `sw.Init != nil` (init clause) and `sw.Tag == nil` (tagless).
  - Emit the tag via `emitExpr`.
  - Walk `sw.Body.List` (each a `*ast.CaseClause`):
    - `cc.List == nil` → the `default` clause. Require exactly one; hold it aside
      for the end.
    - `len(cc.List) != 1` → multi-value case, rejected.
    - the single case value must be a `*ast.BasicLit` (int/string), emitted via
      `emitExpr`; else rejected.
    - `len(cc.Body) == 0` → empty clause, rejected (would emit `V -> ;`, invalid
      Erlang — the 0.3.2 empty-branch bug class).
    - reject a `fallthrough` statement in the body.
    - emit the clause body via `emitBranch(cc.Body)` (own scope).
  - Require a `default` was seen (else "switch needs a default").
  - Emit `case <tag> of <V1> -> <b1>; … ; _ -> <default> end`, the non-default
    clauses in source order, the default last.
- **`emitStmt` — `*ast.TypeSwitchStmt`:** add an explicit positioned rejection
  (deferred to 0.3.4+), rather than the generic `unsupported statement` default.

The per-clause `emitBranch` reuse is the load-bearing correctness point (scoping),
and the empty-clause guard is the load-bearing safety point (invalid Erlang).

## Error handling

All errors positioned via `em.errorf`.

| Condition | Message (intent) |
|---|---|
| `switch` when `!isTail` | control flow is only supported in tail position |
| statements after a `switch` | unreachable statement after a switch |
| tagless `switch` (no tag) | tagless switch is unsupported (0.3.4+); use if |
| `switch` init clause | switch with an init statement is unsupported (0.3.4+) |
| type switch | type switch is unsupported (0.3.4+) |
| multi-value case (`case 1, 2:`) | multi-value case is unsupported (0.3.4+) |
| non-literal case value | case value must be an int or string literal |
| empty clause body | case/default clause has no value (empty body) |
| `fallthrough` | fallthrough is unsupported |
| missing `default` | switch needs a default clause |
| `:=` colliding with an outer bound name in a clause | (existing) already bound |

## Testing

TDD, red → green.

- **Unit tests** (`transpile_test.go`): a multi-case switch with default →
  `case … of … _ -> … end`; default reordered when not last in source; string
  case values → `<<"…">>`; the tag being a receive field (`M.X`); every error
  path (tagless, init, type switch, multi-value, non-literal value, empty clause,
  fallthrough, missing default, non-tail switch, statements-after-switch).
- **Branch-scoping tests:** sibling clauses reuse a `:=` name (accepted); a
  clause `:=` colliding with a parameter (rejected).
- **Real-toolchain rung** (`-tags integration`): a switch-based classifier fixture
  that transpiles, compiles with `erlc`, AND **runs** to a checked result
  (e.g. `classify(2) = "two"`).

## Non-goals / constraints

- Stdlib only. No `pkg/otp` change → SDK index unchanged.
- Deterministic output (clauses in source order, default last).
- `main()` → `run()` untouched; transpiler-internal.

## Key artifacts

- Predecessor: `docs/superpowers/specs/2026-07-12-wintermute-0.3.2-control-flow-design.md`
- Transpiler: `internal/pkg/transpile/transpile.go` (+ `_test.go`); mirror `emitIf`/`emitBranch`.
- Invariant: the `bound-set-integration` memory (switch clauses are a binding context).
- Roadmap context: `HANDOVER.md` ("Next step: 0.3.3 — switch → case-on-value").
