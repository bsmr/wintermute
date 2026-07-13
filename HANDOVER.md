# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-13.

## Current state: 0.3.3 — switch — RELEASED

**0.3.3 is fully released.** `main` @ `5e569f1`, tag `v0.3.3`, on all three
remotes (`origin`, `upstream`, `github`), GitHub release published as Latest,
`production-0.3.3` on `origin`. The last feature step of the 0.3.x transpiler
line to date. Nothing is pending merge.

### 0.3.3 delivered

- **Tagged expression `switch` → Erlang `case`-on-value** (`emitSwitch`, mirrors
  `emitIf`): single literal case values, a required `default` (emitted as the
  catch-all `_`, sorted LAST regardless of Go source position), each clause body
  in its own `bound` scope via `emitBranch`, empty clauses rejected, the switch
  terminal + tail-position only. A type switch (`*ast.TypeSwitchStmt`) is rejected.
  Deferred forms (no-default, multi-value, tagless, type switch, `fallthrough`,
  init) all error → 0.3.4+.
- **Integer-literal normalization**: `emitExpr` normalizes every Go int literal
  to decimal Erlang via `strconv.ParseInt(v, 0, 64)` + `FormatInt(n, 10)`.
  Previously `0777` emitted `0777` (Erlang reads 777 — clean compile, WRONG
  value) and `0x1F` emitted invalid Erlang — a pre-existing verbatim-`BasicLit`
  root cause. Overflow → positioned error, not silent wrap.
- **Exhaustive switch counts as terminating** (folded from the Copilot release
  gate): `terminates()` now accepts a `switch` with a `default` whose every
  clause terminates, so an exhaustive switch may serve as the then-branch of a
  bare `if`, exactly like an if/else. Previously such valid Go was wrongly
  rejected with the "fall through" error (loud, not a mis-transpile).
- **Runnable rung** — `testdata/switch/classify.go` transpiles, compiles with
  `erlc`, and RUNS to `classify:name(2) = "two"`.

Verification (2026-07-13, on `5e569f1`): `go test ./...` green; ladder + cli
integration `-count=1` green (37.3s / 25.3s); Copilot release gate re-run on the
re-squashed diff `25dd628..5e569f1` — **no correctness findings** (fail-safe
confirmed by construction: missing-default / fallthrough / non-terminating-clause
all correctly rejected). The gate's first pass on `25dd628..f0acb53` had found the
`terminates()`/switch gap, which was folded via TDD + re-squash before the push.

## Next line: 0.3.4

Suggested (each starts with `superpowers:brainstorming`):

- **0.3.4 — widen `switch` and/or begin behaviours.** Candidates: `switch`
  without `default` (no-match → the continuation, the bare-`if` continuation
  model); multi-value cases (`case 1, 2:` → an Erlang guard); tagless `switch`
  (→ an if/`case true` chain); **type switch** (`switch v := x.(type)` → type
  guards `is_integer`/…) — note this DOES bind a name (`v`), a full
  `bound-set-integration` context. Or move to full gen_server callbacks
  (`handle_cast`/`handle_info`/`terminate`/`code_change`) / `gen_statem`.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler.

### Open idea: RosettaCode as a differential test corpus

User proposed pulling Go+Erlang solution pairs from https://rosettacode.org/ to
grow the comparison/test set. Not yet actioned — assess in a 0.3.4 brainstorm:
licensing (RosettaCode is GFDL — attribution/scope for vendored fixtures), and
that most tasks exceed the transpiler subset (loops, comprehensions), so only a
curated slice fits. Value: real-world Go→Erlang pairs to pick the next subset
targets from.

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

## Release ritual (reference — for 0.3.4)

The gated-release flow, unchanged from 0.3.1/0.3.2/0.3.3:

1. VERSION bump on `development-X.Y.Z-work`, commit.
2. Resync `-main` to `main` (`git branch -f`), squash-merge `-work`, `commit -s`
   with `feat: Wintermute X.Y.Z — <subtitle>` (write notes to a scratch file,
   reused for the GitHub release). FF `main`, `git tag -a vX.Y.Z`.
3. **Copilot gate** on the release diff `<main-before>..<release-commit>` BEFORE
   the github push — it has found a real bug on every release (0.3.1 receive-field
   collision, 0.3.2 empty case branch, 0.3.3 int literals + the terminates()/switch
   gap). Fold findings via unwind → fix on `-work` (TDD + review) → re-squash →
   re-tag → re-run the gate.
4. Push `main` + tag origin → upstream → github (+ dev branches to origin).
5. `gh release create vX.Y.Z --repo bsmr/wintermute --verify-tag --title … --notes-file … --latest`.
6. `production-X.Y.Z` from `main`, push origin only.
7. Refresh this handover + the README roadmap/coverage/status rows.

## Backlog (deferred)

- **0.3.3 non-blocking nits:** duplicate case values accepted (invalid Go; `erlc`
  warns "cannot match"); the `emitSwitch` `WriteString` string-concat is a
  cosmetic lint (`writestring` vet hint); an `emitCase(subject, clauses)` helper
  would DRY the `emitIf`/`emitSwitch` case-construction duplication.
- **0.3.2 nits:** boolean literals `true`/`false` rejected as lowercase idents
  (so `if true {…}`/`switch true {…}` error — a safe false-negative).
- **Older:** relup/appup hot upgrades; `bin/attach` for the target system;
  native-interop follow-ups (native application module scan, `otp.Apply`, inline
  escape hatch); shared command-preamble DRY; `bin/stop` async-stop residual;
  `absEbin` unescaped in the release `-eval`. See git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. As of
  0.3.3: parameters, `return`, `:=`, calls/recursion, the full operator set,
  `if`/`else` → `case`, tagged `switch` → `case`. Still error (0.3.4+):
  no-default/multi-value/tagless/type `switch`, `else if` chains, side-effect-only
  `if`, guards, multi-value return, cross-module plain calls.
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1), `case` branches (0.3.2), `switch` clauses
  (0.3.3) all snapshot/restore `bound` and reject outer collisions. A **type
  switch's `v :=`** is the next such context.
- **The Copilot release gate keeps finding real bugs** the internal reviews miss —
  run it on the release diff before the github push and fold findings via
  re-squash. Gated remotes receive only tagged releases from `main`, order
  origin→upstream→github, plus a GitHub release per github push.
- `.superpowers/` (SDD ledger) and `bin/` are gitignored scratch.
- `testdata/` Go fixtures are not built by `go test ./...`; only read as source by
  the transpiler / integration tests.

## Key artifacts

- 0.3.3 spec: `docs/superpowers/specs/2026-07-13-wintermute-0.3.3-switch-design.md`
- 0.3.3 plan: `docs/superpowers/plans/2026-07-13-wintermute-0.3.3-switch.md`
- released: `main` @ `5e569f1`, tag `v0.3.3`, `production-0.3.3`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2,3}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.3 — no `pkg/` change)
- Project rules: `CLAUDE.md`
