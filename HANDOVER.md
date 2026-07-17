# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-17.

## ⏭️ RESUME HERE — 0.3.7 is at the brainstorm phase

0.3.6 is **released and closed** (details below); nothing is pending. The next
work is **0.3.7**, which has **no spec or plan yet** — it is at the brainstorm
phase. Start a fresh cycle with `superpowers:brainstorming` (see "Next line:
0.3.7" for the candidate scope), then `writing-plans`, then
`subagent-driven-development` — the exact flow 0.3.6 used. `main` is clean at
`1c9ddfd` (release) plus a following docs commit; `git status` clean, all remotes
in sync. Do NOT treat the 0.3.6 plan (all checkboxes `- [x]`) as unfinished work.

## Current state: 0.3.6 — non-struct type-switch cases + whole-alias V — RELEASED

**0.3.6 is fully released.** `main` @ `1c9ddfd`, tag `v0.3.6`, on all three
remotes (`origin`, `upstream`, `github`), GitHub release published as Latest,
`production-0.3.6` on `origin`. The sixth feature step of the 0.3.x transpiler
line. Nothing is pending merge.

### 0.3.6 delivered

- **Non-struct cases → Erlang type guards.** `case int:`/`string:`/`bool:`/
  `float64:` lower to `V when is_integer(V)` / `is_binary(V)` / `is_boolean(V)` /
  `is_float(V)`. They live in the shared clause builder (`emitCaseClause`, routed
  by a `caseTypeName` that now returns `(name, guard, err)` and a `primitiveGuard`
  map), so **both** the value form (`case X of`) and the receive form (`receive`)
  get them for free. A guarded non-struct clause in a receive is a
  selective-receive clause (blocks on a non-match, never falls through).
- **Whole-alias V.** The switch alias binds the whole matched value: **always**
  for a primitive case (the guard needs the named variable); for a **struct**
  case only when the body uses bare `V` (Erlang alias pattern `V = {tag, Fields}`,
  decided by `bodyUsesBareAlias`); and for the **`default:`** when its body uses
  bare `V` (catch-all binding `V -> Body` — Go's default binds the alias to the
  whole original value). A field-only struct case is byte-identical to 0.3.5
  (`{tag, Fields} ->`, no alias binding). The alias is registered in `em.bound`
  via `registerAlias` (uppercase-checked, collision-rejected).
- **`default:` REQUIRED for the value form** — unchanged from 0.3.5 (a value
  matching no case falls through in Go, which a total Erlang `case` cannot
  express). `seenTag` dedup extended to primitives (duplicate `case int:` → reject).
- **Runnable rung** — `testdata/typeswitch/mixed/classify_mixed.go` (primitive +
  struct + default) transpiles, compiles with `erlc`, and RUNS
  (`TestRung_TypeSwitchMixed`): `classify(7)`→7, `classify({ping,3})`→3,
  `classify(an_atom)`→0.

Deferred to 0.3.7+ (all error loudly): multi-type cases (`case Ping, Pong:`),
tagless `switch M.(type)`, wider primitive types (`int32`, `float32`, `byte`,
`[]byte`, named types), `nil` cases.

### The Copilot release gate — ACCEPT on the first pass (a first for 0.3.x)

Every prior 0.3.x release (0.3.1–0.3.5) had the Copilot gate REJECT once, each
time catching a real silent mis-transpile that internal reviews (incl. opus)
missed. **0.3.6 was the first to ACCEPT on the first pass — no fold needed.** Two
things pre-empted the class the gate hunts: (a) the spec carried an explicit
**disjointness argument** (the four guards + atom-tagged struct tuples are
pairwise disjoint, so Erlang first-match order coincides with Go static
exclusivity), written as a gate template; (b) the **Task-1 internal review** had
already caught this release's silent-mis-transpile candidate — a bare alias in a
`default:` clause would have emitted an *unbound* Erlang variable — and it was
folded into the correct catch-all binding (`V -> Body`) before the gate ran. The
gate verified all five vectors live with `erlc`/`erl` and found nothing further.

Gate MINOR (non-blocking, DEFERRED — see Backlog): a nested type switch reusing
the **same alias name** as its enclosing switch (valid Go shadowing) can trigger
a false "collides with an already-bound name" rejection, because
`bodyUsesBareAlias` walks the outer case body *including nested statements* and
misattributes the inner alias's bare use to the outer. It **fails loud**
(compile-time reject), not silent — a safe over-rejection, not a mis-transpile.

Verification (2026-07-17, on `1c9ddfd`): `go test ./...` green; ladder + cli
integration `-count=1` green (34.9s / 20.3s); govulncheck clean; gosec transpile
pkg 0 issues; gitleaks no leaks. Built subagent-driven/TDD: three tasks
(implementer + spec/quality review each; Task 1 took one fix loop) + whole-branch
opus review + the Copilot gate (ACCEPT).

## Next line: 0.3.7

Suggested (each starts with `superpowers:brainstorming`):

- **0.3.7 — finish widening the type switch, or begin behaviours.** Remaining
  type-switch gaps: **multi-type cases** (`case Ping, Pong:` → multiple clauses
  sharing a body, or a guard disjunction), the **tagless form**
  (`switch M.(type)` with no alias), and **wider primitive types** (`int32`,
  `float32`, `byte`/`[]byte`, named types → more guards). Or move to full
  gen_server callbacks (`handle_cast`/`handle_info`/`terminate`/`code_change`) /
  `gen_statem`. Small self-contained fix worth folding in: scope
  `bodyUsesBareAlias` to stop at a nested `TypeSwitchStmt` boundary (the gate
  MINOR above).

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler. And: a form that would silently mis-transpile is
**rejected loudly**, never emitted.

### Open idea (from earlier session): RosettaCode as a differential test corpus

DEFERRED — see the `rosettacode-access-and-fit` memory: WebFetch UA is blocked
(use the MediaWiki API), and Go+Erlang pairs are poor differential oracles
(idiomatic divergence, subset overflow, GFDL). Useful only as Go-side input
fixtures and a backlog source for which features to widen next.

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration -count=1 ./internal/pkg/ladder/   # -count=1: transpile.go changes, don't trust cache
go test -tags integration -count=1 ./internal/pkg/cli/
```

**Integration-test gotcha:** the CLI/ladder integration tests boot detached BEAM
nodes. A leftover `epmd`/`beam.smp` from a prior run causes odd flaky failures.
Clear first: `pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run. See the
`integration-test-leftover-nodes` memory.

## Release ritual (reference — for 0.3.7)

Unchanged from 0.3.1–0.3.6:

1. VERSION bump on `development-X.Y.Z-work`, commit.
2. Resync `-main` to `main` (`git branch -f`), squash-merge `-work`, `commit -s`
   with `feat: Wintermute X.Y.Z — <subtitle>` (notes to a scratch file, reused
   for the GitHub release; amend to add the `Co-Authored-By` trailer). FF `main`,
   `git tag -a vX.Y.Z`.
3. **Copilot gate** on the release diff `<main-before>..<release-commit>` BEFORE
   the github push (`gh copilot -- -p "<silent-mis-transpile hunt prompt>"
   --allow-all-tools`; it runs long, background it). It found a real
   silent-mis-transpile on 0.3.1–0.3.5; 0.3.6 was the first ACCEPT-first-pass. If
   it REJECTs, fold via unwind → fix on `-work` (TDD) → re-squash → re-tag →
   re-run the gate.
4. Push `main` + tag origin → upstream → github (+ dev branches to origin).
5. `gh release create vX.Y.Z --repo bsmr/wintermute --verify-tag --title … --notes-file … --latest`.
6. `production-X.Y.Z` from `main`, push origin only.
7. Refresh this handover + the README roadmap/coverage/status rows (docs commit,
   origin only — gated remotes get docs at the next tagged release).

## Backlog (deferred)

- **0.3.6 gate MINOR (non-blocking):** `bodyUsesBareAlias` walks nested statements,
  so a nested type switch reusing the enclosing alias name triggers a false
  collision reject (LOUD, not silent). Fix: stop the walk at a nested
  `TypeSwitchStmt` boundary. 0.3.7 candidate.
- **0.3.6 review MINORs (all non-blocking):** no explicit default-branch
  alias-collides-with-param test (covered via the shared `registerAlias` +
  `TestModule_TypeSwitchDefaultLowercaseAliasRejected`); small duplication between
  the default inline block (`emitBranch`) and `emitCaseClause` (`emitStmts`) —
  different snapshot semantics, not trivially merged; `strings.ToLower(name)`
  computed twice in `emitCaseClause` (collision-error path only).
- **Stale version suffix:** the tagless- and init-statement reject messages read
  `(0.3.6+)` (`transpile.go` ~583/590) while multi-type was bumped to `(0.3.7+)`.
  Harmless; align next time those forms are touched.
- **0.3.5 nits (carried):** `structPattern` binds ALL declared fields; a clause
  using only some emits an `erlc` "variable X unused" warning (M-A). Two
  module-wide types sharing a lowercased tag collide silently (`seenTag` is
  per-switch only; M-B). Empty type-switch body `switch V := M.(type) {}` →
  `case M of\nend`, an `erlc` syntax error (LOUD, pre-existing). `WriteString`
  string-concat lint (transpile.go ~562); `rangeint` lint (transpile_test.go:559).
- **Hardening note (0.3.7 watch):** the value operand flows through `emitExpr`, so
  `emitExpr`'s loose `SelectorExpr` handling (`foo.Bar` → bare `Bar`) is reachable
  as a scrutinee — only matters for package-qualified operands (not valid subset
  today).
- **0.3.2 nits:** boolean literals `true`/`false` rejected as lowercase idents;
  unary minus `-1` unsupported.
- **Older:** relup/appup hot upgrades; `bin/attach`; native-interop follow-ups;
  shared command-preamble DRY; `bin/stop` async residual; `absEbin` unescaped in
  the release `-eval`. See git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. As of
  0.3.6: parameters, `return`, `:=`, calls/recursion, the full operator set,
  `if`/`else` → `case`, tagged `switch` → `case`, the type-switch receive, the
  plain-value type switch (`case x of`, default required), non-struct cases over
  `int`/`string`/`bool`/`float64` (→ type guards), and whole-alias `V` (bind the
  whole matched value). Still error (0.3.7+): multi-type cases, tagless switch,
  wider primitives, `nil` case, `else if`, guards, multi-value return,
  cross-module plain calls.
- **A value `case`/type-switch without a default FALLS THROUGH in Go but CRASHES
  in Erlang** (`case_clause`). The value form requires a default (is total); a
  receive does not (it blocks). `terminates()` = `isReceiveTypeSwitch(s) ||
  hasDefault`. See the `typeswitch-value-falls-through` memory.
- **Non-struct guards + struct tuples are pairwise disjoint**, so Erlang
  first-match order coincides with Go static case exclusivity — the 0.3.6 safety
  argument, verified live by the Copilot gate. The default catch-all is always
  emitted last regardless of Go source order.
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1), `case` branches (0.3.2), `switch` clauses
  (0.3.3), type-switch clauses (0.3.4/0.3.5), and the type-switch alias itself
  (0.3.6, via `registerAlias`) all snapshot/restore `bound` and reject collisions.
- **The Copilot release gate keeps finding real silent-mis-transpiles** the
  internal reviews miss (0.3.1–0.3.5; 0.3.6 was the first clean first pass) — run
  it on the release diff before the github push and fold findings via re-squash.
  Gated remotes receive only tagged releases from `main`, order
  origin→upstream→github, plus a GitHub release per github push.
- `.superpowers/` (SDD ledger) and `bin/` are gitignored scratch.
- `testdata/` Go fixtures are not built by `go test ./...`; only read as source by
  the transpiler / integration tests. Struct fixture fields MUST be typed
  (`struct{ Data int }`), never bare `struct{ Data }`. A fixture needing its own
  package goes in its own subdir (e.g. `testdata/typeswitch/mixed/`) to avoid a
  one-package-per-dir clash.

## Key artifacts

- 0.3.6 spec: `docs/superpowers/specs/2026-07-17-wintermute-0.3.6-non-struct-type-switch-cases-design.md`
- 0.3.6 plan: `docs/superpowers/plans/2026-07-17-wintermute-0.3.6-non-struct-type-switch-cases.md`
- released: `main` @ `1c9ddfd`, tag `v0.3.6`, `production-0.3.6`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2,3,6,7}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.6 — no `pkg/` change)
- Project rules: `CLAUDE.md`
