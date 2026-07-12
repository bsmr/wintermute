# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-12.

## Where things stand

**0.3.1 — the value model, first step of the 0.3.x transpiler-language line — is
RELEASED:** squash-merged to `main` = `a6d1575` ("feat: Wintermute 0.3.1 — value
model"), tagged `v0.3.1`, pushed to ALL THREE remotes (`origin`, `upstream`,
`github`); `production-0.3.1` created (origin only). The **GitHub release is
live and marked Latest**: <https://github.com/bsmr/wintermute/releases/tag/v0.3.1>.
`VERSION` is `0.3.1`.

Per the `release-versioning-model`, 0.3.1 is a **feature step** (`X.Y.z`): it
adds real transpiler capability, unlike the 0.3.0 review-and-fix promotion.

### 0.3.1 delivered (transpiler value model — NO control flow yet)

The transpiler (`internal/pkg/transpile/transpile.go`, a go/ast pattern-matcher)
went from a flat "comma-sequence of expression statements" body to one that binds
names and yields a value:

- **Function parameters:** `func Add(X, Y int) int` → `add(X, Y) ->`. Parameter
  names must be uppercase-leading (Erlang variables); lowercase and **unnamed**
  params are rejected, never auto-capitalized. Exported functions export with the
  correct arity `f/N` (was hardcoded `f/0`).
- **Trailing `return`:** `return expr` as the last statement emits `expr` as the
  function value. Early/multi-value return rejected → 0.3.2. A return before a
  receive is correctly rejected as a non-tail early return (`isTail` threading
  through `emitStmts`).
- **Local bindings:** `Z := expr` → Erlang match `Z = expr`. Re-assignment (`=`)
  and rebinding an already-bound name are rejected (immutability).
- **Calls with arguments:** `f(A, B)` → `f(A, B)`; enables self-recursion
  emission (Erlang LCO is free). Non-trivial recursion still needs a base-case
  branch (`case`/`if`, 0.3.2).
- **Real-toolchain rung:** `testdata/valuemodel/math.go` transpiles AND compiles
  with `erlc` to a `.beam` (`TestRung_ValueModel`, `-tags integration`).

### How it was built (subagent-driven, TDD) and what the gate caught

5 subagent-driven tasks (fresh implementer + two-stage review each). One
**Critical caught mid-branch** by review (return-before-receive silently
accepted → `isTail` fix). Whole-branch opus review returned "ready to merge".

**The Copilot gate then earned its keep:** on the release diff it found **2
Critical + 1 Minor silent-mis-transpile bugs the task and final reviews all
missed** — the value-model names were never integrated with the receive-pattern
binding:

- **param name == receive struct-field name** → Erlang treated the pattern
  variable as an *equality match*, not a fresh bind (silent semantic change).
- **`:=` name == receive field name** → runtime `badmatch` (`receiveHead` never
  registered field names into the emitter's `bound` set).
- unnamed param `func F(int, string)` → silent `f/0` arity drop.

Fixed (folded into the squash via re-squash before the github push): `receiveHead`
now registers each field name into `bound` and rejects a collision with any
already-bound name (param / prior `:=` / prior receive field); `paramNames`
rejects unnamed params; a shared `emitArgs` helper dedups the arg-emission loops.
The fix also closed a latent bug (`otp.Receive().([]int)` no longer emits a broken
empty pattern). opus review of the fix: approved, no issues. **Copilot gate #2 on
the corrected diff: clean — no remaining silent-codegen bugs.**

### Verification gate (all green) — 0.3.1, run 2026-07-12 on `a6d1575`

- `go build -o bin/wm ./cmd/wm` clean; `go test ./...` all 5 packages green.
- `go test -tags integration ./internal/pkg/ladder/` green (31.7s, includes the
  new value-model erlc rung); `go test -tags integration ./internal/pkg/cli/`
  green (20s); `pgrep -xc beam.smp` = 0 after (no leaked nodes).
- `govulncheck` clean; `gitleaks` clean; `gosec ./...` = **53 issues / 5 HIGH,
  identical to the 0.3.0 baseline**, all 5 HIGH the accepted G703 class in
  cli/release, **ZERO findings in the transpile package** (0.3.1 is pure string
  emission, no I/O/exec — no new security surface).

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/     # includes TestRung_ValueModel (erlc)
go test -tags integration ./internal/pkg/cli/
```

**Integration-test gotcha (mitigated since 0.3.0):** the CLI/ladder integration
tests boot detached BEAM nodes; a SIGKILL sweep stops a failed `bin/stop` from
leaking one. If a suite still fails oddly after a hard interrupt, clear first:
`pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run. See the
`integration-test-leftover-nodes` memory.

## Next step: 0.3.2 — operators + `if`/`case`/`switch`

0.3.1 is merged, tagged, and on all remotes — nothing pending. The 0.3.x line
continues (each step starts with `superpowers:brainstorming`):

- **0.3.2 — control flow + operators:** comparison (`==` `!=` `<` `>` `<=` `>=`)
  and boolean (`&&` `||` `!`) operators, and `if`/`case`/`switch` → Erlang `case`.
  This is where the flat "comma-sequence" body finally becomes a value-yielding
  tree (0.3.1's Approach A deliberately deferred that abstraction). It also makes
  0.3.1's recursion *useful* (a base-case branch becomes expressible).
- **0.3.3 — full gen_server callbacks** (`handle_cast`/`handle_info`/`terminate`/
  `code_change`) and/or **`gen_statem` / `gen_event`**: mostly marker recognition
  once the language core is in.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler.

## Backlog (deferred)

- **0.3.1 non-blocking nits (from the Copilot gate + reviews, no mis-transpile):**
  - uppercase-parameter check is duplicated: generic in `paramNames`, and inline
    for `HandleCall`'s single parameter — unify behind `paramNames`/a helper.
  - imprecise error wording: compound-assign (`+=`) hits the generic "single-name
    `:=`" message; `_ :=` hits the "uppercase" message. Both correctly rejected.
  - `func F(X, X int)` (duplicate param name — invalid Go, but the transpiler
    doesn't type-check) would emit `f(X, X)`; harden when 0.3.2 touches params.
  - naked `return` (no value) in a typed function is rejected, not emitted as `ok`
    — deliberate per spec ("return must yield exactly one value").
- **relup/appup hot upgrades** — groundwork present (`RELEASES`/`start_erl.data`);
  no `release_handler` upgrade flow, no appup generation.
- **`bin/attach`** for the target system — erts ships `to_erl`/`run_erl`;
  `bin/start` boots via `erl -detached`, so `to_erl` cannot attach.
- **Native-interop follow-ups (from 0.2.7):** native application module scan,
  an `otp.Apply(module, func, args...)` marker, the inline escape hatch (option B).
- **shared command-preamble DRY:** `controlTarget` covers the control-node
  commands; `start`/`release` still have their own preambles.
- **`bin/stop` async-stop residual** and **`absEbin` unescaped** in the release
  `-eval` (self-inflicted, no trust boundary) — see git history for detail.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. 0.3.1
  added parameters/`return`/`:=`/calls-with-args; operators beyond `+`,
  `if`/`case`/`switch`, guards, multi-value return, cross-module plain calls all
  still error (0.3.2+).
- **Receive-field names are Erlang variables** and are now registered in the
  emitter's `bound` set; a param or `:=` colliding with a field is REJECTED (do
  not "fix" this by auto-renaming — the rejection is the correct no-automatism
  behavior).
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from
  `main`, in order origin→upstream→github. **The Copilot review gate runs on the
  release diff before the github push and can find real bugs the internal reviews
  miss — fold its findings via re-squash before pushing.** Handover/doc-only
  commits go to origin and reach the gated remotes with the next release. **On
  every github push, also publish a GitHub release from the tag** (`gh release
  create vX.Y.Z --repo bsmr/wintermute --verify-tag --title "Wintermute X.Y.Z —
  <subtitle>" --notes-file <notes> --latest`; notes = the `feat:` commit body
  minus the `Co-Authored-By` trailer) — see `CLAUDE.md`.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`).
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler / integration tests.

## Key artifacts

- 0.3.1 spec: `docs/superpowers/specs/2026-07-12-wintermute-0.3.1-value-model-design.md`
- 0.3.1 plan: `docs/superpowers/plans/2026-07-12-wintermute-0.3.1-value-model.md`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.1 — no `pkg/` change)
- Project rules: `CLAUDE.md`
