# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-13.

## ⏭️ RESUME HERE — 0.3.5 is at the brainstorm phase

0.3.4 is **released and closed** (details below); nothing is pending. The next
work is **0.3.5**, which has **no spec or plan yet** — it is at the brainstorm
phase. Start a fresh cycle with `superpowers:brainstorming` (see "Next line:
0.3.5" for the candidate scope), then `writing-plans`, then
`subagent-driven-development` — the exact flow 0.3.4 used. `main` is clean at
`ce754b7` (release `7be3b1c` + the docs commit); `git status` clean, all remotes
in sync. Do NOT treat the 0.3.4 plan (all checkboxes `- [x]`) as unfinished work.

## Current state: 0.3.4 — type-switch receive — RELEASED

**0.3.4 is fully released.** `main` @ `7be3b1c`, tag `v0.3.4`, on all three
remotes (`origin`, `upstream`, `github`), GitHub release published as Latest,
`production-0.3.4` on `origin`. The fourth feature step of the 0.3.x transpiler
line. Nothing is pending merge.

### 0.3.4 delivered

- **`switch v := otp.Receive().(type)` → multi-clause Erlang `receive`**
  (`emitTypeSwitchReceive`, gated by `isReceiveTypeSwitch`): the idiomatic
  Erlang process dispatch — a process waits for a message and branches on its
  type. Generalizes the 0.3.1 single-clause receive to N message types.
- **Struct-typed cases only** → tagged tuple `{tag, Field…}`, reusing the
  extracted `structPattern` helper (shared with the single-clause receive). Each
  clause binds its struct's fields in a per-clause `em.bound` scope (snapshot via
  `maps.Clone`, restore after); `v.Field` → `Field`; a bare alias `v` is rejected
  (`em.tsAlias` guard in `emitExpr`). `case *Ping` == `case Ping` (no pointers).
- **`default:` optional**: with → trailing catch-all `_ ->`; without → selective
  receive (non-matching messages stay in the mailbox, the process blocks).
  `terminates()` counts a type-switch receive as terminating with NO default
  (a receive cannot fall through), so it may be a bare-`if` then-branch.
- **Runnable rung** — `testdata/typeswitch/dispatch.go` transpiles, compiles with
  `erlc`, and RUNS: `{ping, …}`→ping clause, `{pong, …}`→pong clause.
- **Two Copilot-gate silent-mis-transpiles folded before release** (the gate
  finds a real one every release): (a) `case Ping` + `case *Ping` (distinct Go
  types, same Erlang tag `ping`) made the second clause unreachable — now any two
  cases sharing a lowercased tag are rejected (covers star-vs-non-star and
  case-only-differing names, i.e. the old opus M-B); (b) a type switch with an
  init statement (`switch n := f(); v := x.(type)`) had its init silently dropped
  — now rejected like `emitIf`/`emitSwitch`.

Deferred to 0.3.5+ (all error): plain-value operand (→ `case`), non-struct cases
(→ guards), multi-type cases, whole aliased `v`, `after`/`fallthrough`/tagless.

Verification (2026-07-13, on `7be3b1c`): `go test ./...` green; ladder + cli
integration `-count=1` green (34.1s / 21.4s, isolated after a leftover-BEAM flake);
Copilot gate re-run on the re-squashed diff `9cf0dd2..7be3b1c` — **fixes correct
and complete, no other silent mis-transpile**. Built subagent-driven/TDD: six
tasks (implementer + spec-and-quality review each) + whole-branch opus review
(ready to merge) + two post-review cleanups (M1 dead `emitStmt` case removed, M2
struct-type check folded into `caseTypeName`).

## Next line: 0.3.5

Suggested (each starts with `superpowers:brainstorming`):

- **0.3.5 — widen the type switch, or begin behaviours.** Candidates: type
  switch over a **plain value** (`switch v := X.(type)` → an Erlang `case` on the
  value, the other lowering); **non-struct cases** (`case int`/`string` → type
  guards `is_integer`/`is_binary`); **multi-type cases** (`case Ping, Pong:`);
  passing the **whole** aliased value. Or move to full gen_server callbacks
  (`handle_cast`/`handle_info`/`terminate`/`code_change`) / `gen_statem`.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler.

### Open idea (from 0.3.4 session): RosettaCode as a differential test corpus

User proposed pulling Go+Erlang solution pairs from https://rosettacode.org/.
Assessed and DEFERRED — see the `rosettacode-access-and-fit` memory: WebFetch UA
is blocked (use the MediaWiki API), and the pairs are poor differential oracles
(the two languages solve each task idiomatically differently, most tasks exceed
the transpiler subset, GFDL license). Useful only as Go-side input fixtures and a
backlog source for which features to widen next.

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
(0.3.4's cli suite flaked once this way with a spurious `-behaviour(gen_server)`
erlc error). Clear first: `pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run.
See the `integration-test-leftover-nodes` memory.

## Release ritual (reference — for 0.3.5)

Unchanged from 0.3.1–0.3.4:

1. VERSION bump on `development-X.Y.Z-work`, commit.
2. Resync `-main` to `main` (`git branch -f`), squash-merge `-work`, `commit -s`
   with `feat: Wintermute X.Y.Z — <subtitle>` (notes to a scratch file, reused
   for the GitHub release). FF `main`, `git tag -a vX.Y.Z`.
3. **Copilot gate** on the release diff `<main-before>..<release-commit>` BEFORE
   the github push — it has found a real silent-mis-transpile on EVERY release
   (0.3.1 receive-field collision, 0.3.2 empty case branch, 0.3.3 non-decimal int
   literals, 0.3.4 tag-collision + dropped init). Fold via unwind → fix on `-work`
   (TDD) → re-squash → re-tag → re-run the gate.
4. Push `main` + tag origin → upstream → github (+ dev branches to origin).
5. `gh release create vX.Y.Z --repo bsmr/wintermute --verify-tag --title … --notes-file … --latest`.
6. `production-X.Y.Z` from `main`, push origin only.
7. Refresh this handover + the README roadmap/coverage/status rows.

## Backlog (deferred)

- **0.3.4 nits:** `structPattern` binds ALL declared fields; a multi-field struct
  whose clause uses only some emits an `erlc` "variable X unused" warning
  (non-pristine, shared with the 0.3.1 receive) — fix both call sites by
  `_`-prefixing unreferenced fields. `receiveHead` rejects `*T` while the type
  switch accepts `*T`→T (harmless: the subset can't construct a pointer value).
  The `TestModule_TypeSwitchReceiveWithDefault` default body emits non-compiling
  Erlang (`Data` unbound in the default) but the test only checks substrings —
  tighten the fixture post-hoc. `emitTypeSwitchReceive`/`emitSwitch` `WriteString`
  string-concat lint; `rangeint` lint at `transpile_test.go:559` (cosmetic).
- **0.3.2 nits:** boolean literals `true`/`false` rejected as lowercase idents.
- **Older:** relup/appup hot upgrades; `bin/attach`; native-interop follow-ups;
  shared command-preamble DRY; `bin/stop` async residual; `absEbin` unescaped in
  the release `-eval`. See git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. As of
  0.3.4: parameters, `return`, `:=`, calls/recursion, the full operator set,
  `if`/`else` → `case`, tagged `switch` → `case`, and `switch v :=
  otp.Receive().(type)` → multi-clause `receive`. Still error (0.3.5+):
  plain-value/non-struct/multi-type type switch, whole-alias `v`, `else if`,
  side-effect-only `if`, guards, multi-value return, cross-module plain calls.
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1), `case` branches (0.3.2), `switch` clauses
  (0.3.3), type-switch clauses (0.3.4) all snapshot/restore `bound` and reject
  outer collisions.
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

- 0.3.4 spec: `docs/superpowers/specs/2026-07-13-wintermute-0.3.4-type-switch-receive-design.md`
- 0.3.4 plan: `docs/superpowers/plans/2026-07-13-wintermute-0.3.4-type-switch-receive.md`
- released: `main` @ `7be3b1c`, tag `v0.3.4`, `production-0.3.4`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2,3}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.4 — no `pkg/` change)
- Project rules: `CLAUDE.md`
