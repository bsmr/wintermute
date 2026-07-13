# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-13.

## Where things stand

**0.3.2 — control flow, the second step of the 0.3.x transpiler-language line —
is RELEASED:** squash-merged to `main` = `ea16579` ("feat: Wintermute 0.3.2 —
control flow"), tagged `v0.3.2`, pushed to ALL THREE remotes (`origin`,
`upstream`, `github`); `production-0.3.2` created (origin only). The **GitHub
release is live and marked Latest**:
<https://github.com/bsmr/wintermute/releases/tag/v0.3.2>. `VERSION` is `0.3.2`.

0.3.2 is a **feature step** (`X.Y.z`): it makes 0.3.1's recursion useful by adding
operators and control flow.

### 0.3.2 delivered (transpiler control flow)

The flat function body became a **value-yielding tree** (an `if` in tail position
emits a `case`), and the full operator set landed:

- **Operators** — arithmetic `- *`, `/`→`div`, `%`→`rem`; comparison `==`→`=:=`,
  `!=`→`=/=`, `< > >=`, `<=`→`=<` (exact term equality, no coercion); boolean
  `&&`→`andalso`, `||`→`orelse`, unary `!`→`not`. A binary operand that is itself
  a binary expression is parenthesized (`emitOperand`/`unparen`), so Go's grouping
  survives regardless of Erlang precedence. `ParenExpr` is unwrapped.
- **`if` → Erlang `case`** — if/else and the bare-`if`-plus-continuation base case
  (`if N==0 { return 1 }; return N*Fact(N-1)` → `case N =:= 0 of true -> 1;
  false -> N * fact(N - 1) end`). **Each `case` branch is emitted in its own
  binding scope**: `em.bound` is snapshotted (`maps.Clone`) and restored around
  each branch (`emitBranch`), so sibling clauses may reuse a name freshly while a
  collision with an outer binding — a parameter, a `:=`, or a **receive-pattern
  field** — is still rejected. A `terminates()` helper rejects a bare-`if`
  then-branch that would fall through. **Empty `if`/`else` branches are rejected**
  (they would emit an invalid `true -> ;` case clause).
- **Runnable rung** — `testdata/controlflow/fact.go` (recursive factorial)
  transpiles, compiles with `erlc`, AND **runs** to `fact(5) = 120`.

Branch scoping was built per the `bound-set-integration` invariant (the class the
0.3.1 Copilot gate caught for receive patterns); the branch × receive-field seam
is now explicitly tested.

### How it was built and what the gate caught

4 subagent-driven tasks (fresh implementer + two-stage review each). The
whole-branch opus review returned "ready to merge" after independently tracing
the receive-field × branch-binding intersection and confirming rejection, and its
recommended seam test was folded.

**The Copilot gate again found a real bug the internal reviews missed:** an
**empty `if`/`else` branch** emitted invalid Erlang (`case … of true -> ; … end`)
silently — reachable via a valid void function. Fixed (folded via re-squash before
the github push): `emitIf` rejects empty then/else blocks with a positioned
"empty block" error. Copilot gate #2 on the corrected diff: clean, no remaining
silent-wrong findings.

### Verification gate (all green) — 0.3.2, run 2026-07-13 on `ea16579`

- `go build -o bin/wm ./cmd/wm` clean; `go test ./...` all 5 packages green.
- `go test -tags integration ./internal/pkg/ladder/` green (31.7s, includes the
  factorial-runs-to-120 rung); `go test -tags integration ./internal/pkg/cli/`
  green (20s); `pgrep -xc beam.smp` = 0 after (no leaked nodes).
- `govulncheck` clean; `gitleaks` clean; `gosec ./...` = **53 issues / 5 HIGH,
  identical to the 0.3.1 baseline**, all 5 HIGH the accepted G703 class in
  cli/release, **ZERO findings in the transpile package** (pure string emission,
  no new security surface).

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/     # includes TestRung_ControlFlowRecursion (runs fact(5)=120)
go test -tags integration ./internal/pkg/cli/
```

**Integration-test gotcha (mitigated since 0.3.0):** the CLI/ladder integration
tests boot detached BEAM nodes; a SIGKILL sweep stops a failed `bin/stop` from
leaking one. If a suite fails oddly after a hard interrupt, clear first:
`pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run. See the
`integration-test-leftover-nodes` memory.

## Next step: 0.3.3 — `switch` → `case`-on-value (then behaviours)

0.3.2 is merged, tagged, and on all remotes — nothing pending. Suggested cut (each
starts with `superpowers:brainstorming`):

- **0.3.3 — `switch` → Erlang `case`-on-value:** the expression switch
  (`switch x { case 1: …; default: … }` → `case x of 1 -> …; _ -> … end`) reuses
  the 0.3.2 `case`/branch-scoping machinery, so it is mostly a new statement form
  + clause emission. Decide the tagless-switch (→ if-chain) and type-switch (→
  guards) scope then. **Note the `bound-set-integration` invariant applies again:**
  switch-clause patterns bind Erlang variables — register + scope them exactly
  like receive fields and `case` branches, or the 0.3.1/0.3.2-class collision bug
  recurs.
- **0.3.4+ — full gen_server callbacks** (`handle_cast`/`handle_info`/`terminate`/
  `code_change`) and/or **`gen_statem` / `gen_event`**: mostly marker recognition
  once the language core is complete.

Framing (unchanged): the transpiler covers only what maps cleanly to Erlang;
loops, list comprehensions, mutable state stay in the native-`.erl` escape hatch
(0.2.7), NOT the transpiler.

## Backlog (deferred)

- **0.3.2 non-blocking nits:**
  - **boolean literals `true`/`false`** are rejected as lowercase-leading idents
    (so `if true { … }` errors) — a safe false-negative (errors, never
    mis-transpiles), but a comfort gap; special-case them as Erlang atoms in 0.3.3.
  - **if/else branches skip `terminates()`** — a non-terminating (but non-empty)
    if/else branch in a void function yields `ok`, which is correct Go semantics,
    but the spec calls side-effect-only `if` deferred. Route if/else branches
    through `terminates()` for defense-in-depth when `go/types` integration lands.
  - `switch` is rejected via the generic `emitStmt` default rather than a
    roadmap-pointed message (cosmetic).
- **relup/appup hot upgrades**; **`bin/attach`** for the target system;
  **native-interop follow-ups** (native application module scan, `otp.Apply`,
  inline escape hatch); **shared command-preamble DRY** (`start`/`release`);
  **`bin/stop` async-stop residual**; **`absEbin` unescaped** in the release
  `-eval` — all detailed in git history / prior handovers.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler subset only** — errors on anything outside it, by design. 0.3.2
  added operators + `if`; `switch`, `else if` chains, side-effect-only `if`,
  guards, multi-value return, cross-module plain calls all still error (0.3.3+).
- **New binding contexts must integrate with `em.bound`** (`bound-set-integration`
  memory): receive fields (0.3.1) and `case` branches (0.3.2) both register into
  `bound` and reject outer collisions / scope siblings. 0.3.3's switch patterns
  are the next such context — do the same or the collision bug recurs.
- **The Copilot release gate keeps finding real silent-mis-transpile bugs the
  internal reviews miss** (0.3.1: receive-field collision; 0.3.2: empty case
  branch). Run it on the release diff before the github push and **fold findings
  via re-squash before pushing.** Gated remotes (`upstream`, `github`) receive
  only tagged releases from `main`, in order origin→upstream→github. **On every
  github push, also publish a GitHub release from the tag** (`gh release create
  vX.Y.Z --repo bsmr/wintermute --verify-tag --title "Wintermute X.Y.Z —
  <subtitle>" --notes-file <notes> --latest`; notes = the `feat:` commit body
  minus the `Co-Authored-By` trailer) — see `CLAUDE.md`.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`).
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler / integration tests.

## Key artifacts

- 0.3.2 spec: `docs/superpowers/specs/2026-07-12-wintermute-0.3.2-control-flow-design.md`
- 0.3.2 plan: `docs/superpowers/plans/2026-07-12-wintermute-0.3.2-control-flow.md`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2}-wintermute-0.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.2 — no `pkg/` change)
- Project rules: `CLAUDE.md`
