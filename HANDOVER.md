# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-17.

## ⏭️ RESUME HERE — 0.3.6 is at the brainstorm phase

0.3.5 is **released and closed** (details below); nothing is pending. The next
work is **0.3.6**, which has **no spec or plan yet** — it is at the brainstorm
phase. Start a fresh cycle with `superpowers:brainstorming` (see "Next line:
0.3.6" for the candidate scope), then `writing-plans`, then
`subagent-driven-development` — the exact flow 0.3.5 used. `main` is clean at
`1e23a8c` (release) plus a following docs commit; `git status` clean, all remotes
in sync. Do NOT treat the 0.3.5 plan (all checkboxes `- [x]`) as unfinished work.

## Current state: 0.3.5 — plain-value type switch — RELEASED

**0.3.5 is fully released.** `main` @ `1e23a8c`, tag `v0.3.5`, on all three
remotes (`origin`, `upstream`, `github`), GitHub release published as Latest,
`production-0.3.5` on `origin`. The fifth feature step of the 0.3.x transpiler
line. Nothing is pending merge.

### 0.3.5 delivered

- **`switch V := X.(type)` over any value → Erlang `case X of {tag, Field…} ->
  body; … end`** (`emitTypeSwitchValue`): the value-branching counterpart to the
  0.3.4 receive dispatch. A new `emitTypeSwitch` entry point dispatches on the
  operand — `otp.Receive()` → `receive`, any other value → `case X of`.
- **Struct-typed cases only**, reusing the shared clause builder. The core is a
  behaviour-preserving refactor: the 0.3.4 receive clause loop was extracted into
  **`emitTypeSwitchClauses`** (returns `(clauses, haveDefault, err)`), now shared
  by both wrappers via a small `wrapClauses(header, clauses)` helper.
- **`default:` REQUIRED for the value form** (this is the release's key
  correction — see the Copilot-gate note below). A value matching no case *falls
  through* in Go (ordinary control flow), which a total Erlang `case` cannot
  express, so a default-less value switch is **rejected** ("a plain-value type
  switch requires a default clause"). The receive form keeps `default:` optional
  (a selective receive blocks on a non-match, never falls through).
- **`terminates()` distinguishes the two forms**: `isReceiveTypeSwitch(s) ||
  hasDefault` — a receive terminates without a default, a value switch only with
  one (so a default-carrying value switch may be a bare-`if` then-branch).
- **Operand/alias are uppercase Erlang variables** (`M any`, `V := M.(type)`); a
  lowercase operand is rejected, never silently emitted as an atom. `V.Field`
  resolves to the bound field; a bare alias `V` is rejected.
- **Runnable rung** — `testdata/typeswitch/classify.go` transpiles, compiles with
  `erlc`, and RUNS: `Classify({ping,1})`→1, `Classify({pong,2})`→2.

Deferred to 0.3.6+ (all error loudly): non-struct cases (→ guards), multi-type
cases, whole-alias `V`, tagless `switch M.(type)`, init statement.

### ⚠️ The Copilot release gate caught the 0.3.5 silent mis-transpile (as every release)

The spec originally chose **`default:` optional, let-it-crash** for the value
form. The Copilot gate proved (with live `erlc`/`erl`) that this was a **silent
mis-transpile**: a default-less value type switch *falls through* in Go when no
case matches (e.g. `Classify({pong,2}, true)` returns the continuation `99`), but
was emitted as a total Erlang `case` with no catch-all, raising `case_clause` —
Go returns normally where Erlang crashes. The opus whole-branch review missed it
too (it accepted the "documented divergence" framing). The fold made `default:`
**required** for the value form (fixing `terminates()` to distinguish receive
from value). **Lesson (see the `typeswitch-value-falls-through` memory): a value
`case`/type-switch without a default is NOT equivalent to a selective receive —
Go falls through, Erlang crashes; the value form must be total.**

Verification (2026-07-17, on `1e23a8c`): `go test ./...` green; ladder + cli
integration `-count=1` green (33.0s / 19.7s); Copilot gate re-run on the fixed
release diff `e3596d3..1e23a8c` — **ACCEPT** (fix correct + complete, no new
silent mis-transpile). Built subagent-driven/TDD: four tasks (implementer + spec
review each) + whole-branch opus review + the Copilot-gate fold.

## Next line: 0.3.6

Suggested (each starts with `superpowers:brainstorming`):

- **0.3.6 — widen the type switch further, or begin behaviours.** The natural
  next step is **non-struct cases** (`case int:`/`string:` → Erlang type guards
  `is_integer`/`is_binary`), which now land most cleanly in the value form
  (`case X of N when is_integer(N) ->`). Other candidates: **multi-type cases**
  (`case Ping, Pong:`), **whole-alias `V`** (bind the entire matched value). Or
  move to full gen_server callbacks (`handle_cast`/`handle_info`/`terminate`/
  `code_change`) / `gen_statem`.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler. And: a form that would silently mis-transpile (like
the default-less value switch) is **rejected loudly**, never emitted.

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
nodes. A leftover `epmd`/`beam.smp` from a prior run causes odd flaky failures
(0.3.5's cli suite flaked once this way — a first run saw leftovers from the
ladder run; a clean re-run passed). Clear first: `pkill -9 -x beam.smp; pkill -9
-x epmd`, then re-run. See the `integration-test-leftover-nodes` memory.

## Release ritual (reference — for 0.3.6)

Unchanged from 0.3.1–0.3.5:

1. VERSION bump on `development-X.Y.Z-work`, commit.
2. Resync `-main` to `main` (`git branch -f`), squash-merge `-work`, `commit -s`
   with `feat: Wintermute X.Y.Z — <subtitle>` (notes to a scratch file, reused
   for the GitHub release). FF `main`, `git tag -a vX.Y.Z`.
3. **Copilot gate** on the release diff `<main-before>..<release-commit>` BEFORE
   the github push — it has found a real silent-mis-transpile on EVERY release
   (0.3.1 receive-field collision, 0.3.2 empty case branch, 0.3.3 non-decimal int
   literals, 0.3.4 tag-collision + dropped init, 0.3.5 default-less value switch
   falling through). Fold via unwind → fix on `-work` (TDD) → re-squash → re-tag
   → re-run the gate.
4. Push `main` + tag origin → upstream → github (+ dev branches to origin).
5. `gh release create vX.Y.Z --repo bsmr/wintermute --verify-tag --title … --notes-file … --latest`.
6. `production-X.Y.Z` from `main`, push origin only.
7. Refresh this handover + the README roadmap/coverage/status rows (docs commit,
   origin only — gated remotes get docs at the next tagged release).

## Backlog (deferred)

- **0.3.5 nits (all non-blocking, from the Copilot gate + reviews):** `structPattern`
  binds ALL declared fields; a clause using only some emits an `erlc` "variable X
  unused" warning (non-pristine, shared with the 0.3.1 receive) — fix both call
  sites by `_`-prefixing unreferenced fields (carried M-A). Two module-wide types
  sharing a lowercased tag collide silently (`seenTag` is per-switch only; carried
  M-B). Empty type-switch body `switch V := M.(type) {}` → `case M of\nend`, an
  `erlc` syntax error (LOUD, pre-existing, shared with `receive\nend`); a
  transpiler-level "no clauses" error would be nicer. The `WriteString`
  string-concat lint remains at `emitSwitch` (transpile.go ~562, 0.3.3 backlog);
  `rangeint` lint at `transpile_test.go:559` (cosmetic).
- **Hardening note (0.3.6 watch):** the value operand now flows through `emitExpr`,
  so `emitExpr`'s loose `SelectorExpr` handling (`foo.Bar` → bare `Bar`) is newly
  reachable as a scrutinee — only matters for package-qualified operands (not valid
  subset today), but keep it in view when adding non-struct guards.
- **0.3.2 nits:** boolean literals `true`/`false` rejected as lowercase idents;
  unary minus `-1` unsupported (surfaced while building 0.3.5 repros).
- **Older:** relup/appup hot upgrades; `bin/attach`; native-interop follow-ups;
  shared command-preamble DRY; `bin/stop` async residual; `absEbin` unescaped in
  the release `-eval`. See git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. As of
  0.3.5: parameters, `return`, `:=`, calls/recursion, the full operator set,
  `if`/`else` → `case`, tagged `switch` → `case`, the type-switch receive
  (`switch v := otp.Receive().(type)` → multi-clause `receive`), and the
  plain-value type switch (`switch v := x.(type)` → `case x of`, default
  required). Still error (0.3.6+): non-struct/multi-type type switch, whole-alias
  `v`, tagless switch, `else if`, side-effect-only `if`, guards, multi-value
  return, cross-module plain calls.
- **A value `case`/type-switch without a default FALLS THROUGH in Go but CRASHES
  in Erlang** (`case_clause`) — they are not equivalent. The value form requires a
  default (is total); a receive does not (it blocks). This is the `terminates()`
  distinction (`isReceiveTypeSwitch(s) || hasDefault`). See the
  `typeswitch-value-falls-through` memory.
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1), `case` branches (0.3.2), `switch` clauses
  (0.3.3), type-switch clauses (0.3.4/0.3.5) all snapshot/restore `bound` and
  reject outer collisions.
- **The Copilot release gate keeps finding real silent-mis-transpiles** the
  internal reviews (incl. opus) miss — run it on the release diff before the
  github push and fold findings via re-squash. Gated remotes receive only tagged
  releases from `main`, order origin→upstream→github, plus a GitHub release per
  github push.
- `.superpowers/` (SDD ledger) and `bin/` are gitignored scratch.
- `testdata/` Go fixtures are not built by `go test ./...`; only read as source by
  the transpiler / integration tests. Struct fixture fields MUST be typed
  (`struct{ Data int }`), never bare `struct{ Data }` (Go embedded field → zero
  registered fields).

## Key artifacts

- 0.3.5 spec: `docs/superpowers/specs/2026-07-16-wintermute-0.3.5-plain-value-type-switch-design.md`
  (revised in-place: default-required, with a Copilot-gate revision note).
- 0.3.5 plan: `docs/superpowers/plans/2026-07-16-wintermute-0.3.5-plain-value-type-switch.md`
- released: `main` @ `1e23a8c`, tag `v0.3.5`, `production-0.3.5`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2,3,6}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.5 — no `pkg/` change)
- Project rules: `CLAUDE.md`
