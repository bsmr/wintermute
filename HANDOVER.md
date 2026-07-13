# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-13.

## ⏭️ RESUME HERE — 0.3.3 is BUILT & VERIFIED but NOT YET RELEASED

**The 0.3.3 (switch) work is complete, green, and whole-branch-review-approved,
sitting on `development-0.3.3-work` @ `04767c9` (also pushed to `origin`). It is
NOT merged to `main`, NOT tagged, NOT pushed to the gated remotes. The next
action is the finishing flow.** `main` is still at `0866904` (0.3.3 spec + plan
only); the last RELEASED version is **0.3.2** (`ea16579`, all three remotes).

### Do this next: the 0.3.3 finishing flow

Follow the project's gated-release ritual (see `CLAUDE.md`), exactly as 0.3.1/0.3.2:

1. **VERSION → `0.3.3`** on `development-0.3.3-work`, commit (`chore: bump VERSION to 0.3.3`).
2. **Squash to main.** `git branch -f development-0.3.3-main main` (resync so the
   fast-forward is clean — `main` may hold docs commits `-main` lacks), then
   `git checkout development-0.3.3-main`, `git merge --squash development-0.3.3-work`,
   `git commit -s` with subject **`feat: Wintermute 0.3.3 — switch`** and a body
   describing the feature (write release notes to a scratch file first — reused
   for the GitHub release, minus the `Co-Authored-By` trailer). Then
   `git checkout main`, `git merge --ff-only development-0.3.3-main`, and
   `git tag -a v0.3.3 -m "Wintermute 0.3.3 — switch"`.
3. **Copilot gate on the release diff BEFORE the github push.** It has found a
   REAL silent-mis-transpile bug on every release so far (0.3.1 receive-field
   collision, 0.3.2 empty case branch, 0.3.3 non-decimal int literals — the last
   caught by the internal opus review this time). Run:
   `gh copilot -- -p "Review the git diff of the range <main-before>..<release-commit> …" --allow-all-tools`
   and **fold any findings via re-squash before pushing** (unwind the local
   merge/tag, fix on `-work` with TDD + review, re-squash, re-tag, re-run the gate).
4. **Push** `main` + tag `v0.3.3` in order `origin` → `upstream` → `github`
   (and the `development-0.3.3-*` branches to `origin` as archive — already done).
5. **GitHub release** from the tag:
   `gh release create v0.3.3 --repo bsmr/wintermute --verify-tag --title "Wintermute 0.3.3 — switch" --notes-file <notes> --latest`.
6. **`production-0.3.3`** from `main`, push to `origin` only.
7. **Refresh this handover** to the released state (mirror the 0.2.7→0.3.2 pattern),
   and add a `README.md` roadmap/coverage/status row for 0.3.3 (the README is
   updated per-release; it currently shows `0.3.2`).

The full per-task detail lives in the SDD ledger
`.superpowers/sdd/progress.md` (gitignored, local to the build machine) — read it
if present; otherwise this section plus `git log development-0.3.3-work` is enough.

### 0.3.3 delivered (on `development-0.3.3-work`, pending release)

- **Tagged expression `switch` → Erlang `case`-on-value** (`emitSwitch`, mirrors
  `emitIf`): single literal case values, a required `default` (emitted as the
  catch-all `_`, sorted LAST regardless of Go source position), each clause body
  in its own `bound` scope via `emitBranch`, empty clauses rejected, the switch
  terminal + tail-position only. A type switch (`*ast.TypeSwitchStmt`) is rejected
  in `emitStmt`. Deferred forms (no-default, multi-value cases, tagless, type
  switch, `fallthrough`, init) all error → 0.3.4+.
- **Integer-literal normalization** (from the Task-1 review's Critical): `emitExpr`
  now normalizes every Go int literal to decimal Erlang via
  `strconv.ParseInt(v, 0, 64)` + `FormatInt(n, 10)`. Previously `0777` emitted
  `0777` (Erlang reads 777 — clean compile, WRONG value) and `0x1F` emitted
  invalid Erlang; this was a PRE-EXISTING root cause (verbatim `BasicLit.Value`)
  affecting `return 0x1F` too. Overflow → positioned error, not silent wrap.
- **Runnable rung** — `testdata/switch/classify.go` transpiles, compiles with
  `erlc`, and RUNS to `classify:name(2) = "two"`.

Verification (2026-07-13, on `04767c9`): `go test ./...` green; ladder integration
forced `-count=1` green (31.8s, incl. the switch rung); cli integration green
(19.9s, after clearing a leftover-epmd flake — see `integration-test-leftover-nodes`);
`pgrep beam.smp`=0; govulncheck/gitleaks clean; gosec 53/5 HIGH identical to the
0.3.2 baseline, ZERO findings in the transpile package. Final whole-branch opus
review: **ready to merge**; its two recommended coverage tests (string case value,
receive-field switch tag) were folded (`04767c9`).

---

## Last released: 0.3.2 — control flow

0.3.2 (`ea16579`, tag `v0.3.2`, all three remotes, `production-0.3.2`, GitHub
release Latest) added operators + `if` → Erlang `case`, turning the flat function
body into a value-yielding tree and making recursion useful. See the git history
and the GitHub releases page for the 0.1.0 → 0.3.2 line.

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration -count=1 ./internal/pkg/ladder/   # -count=1: transpile.go changed, don't trust the cache
go test -tags integration -count=1 ./internal/pkg/cli/
```

**Integration-test gotcha:** the CLI/ladder integration tests boot detached BEAM
nodes. A leftover `epmd`/`beam.smp` from a prior run causes odd flaky failures
(0.3.3's cli suite flaked once this way). Clear first:
`pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run. See the
`integration-test-leftover-nodes` memory.

## Next line after 0.3.3 ships: 0.3.4

Suggested (each starts with `superpowers:brainstorming`):

- **0.3.4 — widen `switch` and/or begin behaviours.** Candidates: `switch`
  without `default` (the no-match case → the continuation, the bare-`if`
  continuation model); multi-value cases (`case 1, 2:` → an Erlang guard); tagless
  `switch` (→ an if/`case true` chain); **type switch** (`switch v := x.(type)` →
  type guards `is_integer`/…) — note this DOES bind a name (`v`), a full
  `bound-set-integration` context. Or move to full gen_server callbacks
  (`handle_cast`/`handle_info`/`terminate`/`code_change`) / `gen_statem`.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler.

## Backlog (deferred)

- **0.3.3 non-blocking nits (roll-up, no valid-Go mis-transpile):** duplicate case
  values accepted (invalid Go; `erlc` warns "cannot match"); a non-terminating
  clause body accepted (invalid Go; consistent with if/else-branch leniency); the
  `emitSwitch` `WriteString` string-concat is a cosmetic lint.
- **0.3.2 nits:** boolean literals `true`/`false` rejected as lowercase idents
  (so `if true { … }`/`switch true {…}` error — a safe false-negative, comfort
  gap); if/else branches skip `terminates()` (correct output, defense-in-depth for
  when `go/types` lands).
- **Older:** relup/appup hot upgrades; `bin/attach` for the target system;
  native-interop follow-ups (native application module scan, `otp.Apply`, inline
  escape hatch); shared command-preamble DRY; `bin/stop` async-stop residual;
  `absEbin` unescaped in the release `-eval`. See git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. As of
  0.3.3: parameters, `return`, `:=`, calls/recursion, the full operator set,
  `if`/`else` → `case`, and the tagged `switch` → `case`. Still error (0.3.4+):
  no-default/multi-value/tagless/type `switch`, `else if` chains, side-effect-only
  `if`, guards, multi-value return, cross-module plain calls.
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1), `case` branches (0.3.2), and `switch` clauses
  (0.3.3) all snapshot/restore `bound` and reject outer collisions. A **type
  switch's `v :=`** is the next such context — do the same or the collision bug
  recurs.
- **The Copilot release gate keeps finding real silent-mis-transpile bugs** the
  internal reviews miss — run it on the release diff before the github push and
  fold findings via re-squash. Gated remotes (`upstream`, `github`) receive only
  tagged releases from `main`, in order origin→upstream→github. On every github
  push, also publish a GitHub release from the tag (see `CLAUDE.md`).
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`).
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler / integration tests.

## Key artifacts

- 0.3.3 spec: `docs/superpowers/specs/2026-07-13-wintermute-0.3.3-switch-design.md`
- 0.3.3 plan: `docs/superpowers/plans/2026-07-13-wintermute-0.3.3-switch.md`
- 0.3.3 branch (unreleased): `development-0.3.3-work` @ `04767c9` (on `origin`)
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2,3}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.3 — no `pkg/` change)
- Project rules: `CLAUDE.md`
