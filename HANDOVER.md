# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-12.

## Where things stand

**0.2.7 native Erlang interop (whole-module escape hatch) is RELEASED:
squash-merged to `main` = `7c268cb`, tagged `v0.2.7` (annotated `2870427`),
pushed to ALL THREE remotes (`origin`, `upstream`, `github`); `production-0.2.7`
created (origin only — upstream/github carry no `production-*`).** It is the
eighth step of the 0.2.x line and the first that touches the build to add a NEW
input kind (hand-written `.erl`), rather than a deployment step. The 5-task build
was subagent-driven (fresh implementer + two-stage review per task); the final
whole-branch review (opus) returned "ready to merge" with two doc-only nits
(folded, `e40c1b1`). The Copilot gate then surfaced three items — all folded via
re-squash before the github push (see below).

0.2.5 (`v0.2.5`), 0.2.6 (`v0.2.6`), 0.2.0–0.2.4, and 0.1.0 are all released on
`main` and the relevant remotes.

The completed 0.2.x line is promoted to **0.3.0** after a further review + fix
session (see the `release-versioning-model` memory) — still pending.

### 0.2.7 delivered

- **`wm build`/`wm release` accept hand-written `.erl` modules** alongside `.go`
  sources, for constructs the transpiler subset cannot express but OTP needs
  (records, macros, complex guards, binary pattern matching, list comprehensions).
  Wintermute is now "Go-first but Erlang-capable" — an escape hatch.
- **Mechanism (one integration point — `buildApp`, `internal/pkg/cli/cli.go`,
  shared by build and release):** a `.erl` input bypasses the transpiler, is
  validated (basename = module name; `validAppName` rejects `/`,`\`,`..`; a new
  `erlModuleName` scan rejects a `-module(x)`/filename mismatch fail-fast), copied
  through via the shared `writeModule` helper (overwrite-guarded), and listed in
  `modules`. Downstream `erlc` + `.app`/`.rel` generation is unchanged.
- **Native modules are non-application by scope:** the application and supervisor
  modules stay transpiled Go. `appMod`/`registered` are never set for `.erl`.
- **Interop uses existing OTP mechanisms, no new marker:** a native gen_server
  registered `{global, Name}` is reached from transpiled Go via
  `otp.CallGlobal("Name", ...)`. The registered-name string was already decoupled.
- **New code:** the `.erl` branch + `writeModule` + `erlModuleName`/`erlModuleRe`
  + `nativeErlUsageHint` const in `cli.go`; usage strings in `cli.go`/`release.go`;
  README "Native Erlang modules (escape hatch)" subsection. **No transpiler
  change, no `pkg/otp` change** (SDK index unchanged).
- **Fixture:** `testdata/native/echoserver.erl` — a gen_server using a `-record`
  and a guard (constructs Go can't emit), registering `{global, echo}`, drop-in
  for the persistent fixture's Go supervisor child spec `{echoserver, start, []}`.

### Testing (all green on real OTP 29.0.3)

- **Unit (`internal/pkg/cli/cli_test.go`):** buildApp `.erl` routing (accept,
  collision-refusal, invalid-name), the `-module`/filename mismatch rejection, and
  a commented-`-module` robustness case.
- **CLI integration (`internal/pkg/cli/native_integration_test.go`, rung
  analog):** real `wm release --self-contained` with a mixed input set
  `[persistent go echoapp, go echosup, native echoserver.erl]`, unpacked, native
  `echoserver.beam` confirmed packaged, booted under a fully scrubbed environment,
  scrubbed control node resolves `{global, echo}` → `hello`.
- **Ladder rung VIII (`internal/pkg/ladder/ladder_native_integration_test.go`):**
  native server inside a supervised release (Go app/sup transpiled), transpiled-Go
  client calls it → `hello`. `mixedNativeApp` mirrors `transpilePersistentApp`.

### Verification gate (all green) — 0.2.7, run 2026-07-12 on final `main` (7c268cb)

- `go build -o bin/wm ./cmd/wm` — clean; `go test ./...` — all 5 packages green.
- `go test -tags integration ./internal/pkg/ladder/` — 24 rungs (I–VII + tarball +
  new VIII), 31s.
- `go test -tags integration ./internal/pkg/cli/` — 0.2.5 e2e + rung VII +
  new native e2e, 21s.
- `govulncheck ./...` clean; `gitleaks detect` clean.
- `gosec ./...` — **5 HIGH, all `G703`** (path-traversal-via-taint), the same
  accepted dual-use class as 0.2.4–0.2.6 (user-supplied path in a local CLI). The
  one native-branch HIGH is the `validAppName`-guarded `.erl` copy (guard sits
  before the tainted path; gosec's taint analysis can't see through the validator).
  No new unaccepted HIGH/CRITICAL category.

### Copilot review gate (pre-github) — three findings, all folded

Run on the release diff. No HIGH defects; security confirmed correct. Folded via
re-squash (`-work` fold-in `a4b9983` → fresh `-main` `7c268cb`):
- **Medium:** buildApp did not cross-check a native `.erl`'s `-module(x)` against
  its filename. `erlc` catches it in `wm release`, but `wm build` never erlc's, so
  it could emit an `.app` inconsistent with the source. Fixed: `erlModuleName`
  (comment/blank-line-aware scan) rejects a definitive mismatch fail-fast.
- **Low (DRY):** the `.erl` usage suffix was duplicated in the build/release usage
  strings → shared `nativeErlUsageHint` const.
- **Low (KISS):** the overwrite-guard + `WriteFile` pattern was duplicated across
  the transpiled and native branches → `writeModule` helper.

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/     # 24 rungs
go test -tags integration ./internal/pkg/cli/        # 0.2.5 e2e + rung VII + native e2e
# Native .erl release by hand:
./bin/wm release app.go sup.go server.erl --out DIR  # server.erl compiled + packaged natively
```

**Integration-test gotcha (cost time this cycle):** the CLI/ladder integration
tests boot detached BEAM nodes and their `t.Cleanup` discards `bin/stop`'s error,
so repeated local runs LEAK `beam.smp` nodes that pollute epmd and eventually make
a run FAIL for environmental reasons (not a code regression). If an integration
suite fails oddly, first: `pkill -9 -f '/tmp/Test'` (the leftover nodes) and
`pkill -9 -x epmd`, then re-run. See the `integration-test-leftover-nodes` memory.

## Next step: pick the next milestone (0.2.7 is shipped)

0.2.7 is merged, tagged, and on all remotes — nothing pending. The
`native-erlang-interop-open-question` is now RESOLVED (whole-module cut shipped).
Candidates for next (no decision made — start with `superpowers:brainstorming`):

- **0.3.0 promotion** of the whole 0.2.x line (single-node → distributed →
  gen_server → application → persistent node → release → self-contained target
  system → native interop).
- **Native interop follow-ups** (deferred in the 0.2.7 spec, tracked in Backlog):
  native application module (behaviour scan), `otp.Apply(module, func, args...)`
  marker for direct Go→pure-native-function calls, and the inline escape hatch
  (option B — embed raw Erlang for individual expressions inside a Go module).
- **relup/appup hot upgrades** — 0.2.6 groundwork (`RELEASES`, `start_erl.data`);
  the upgrade flow itself is still unbuilt.

## 0.2.x backlog (deferred)

- **Native interop — deferred parts (from the 0.2.7 spec):**
  - **Native application module:** allow a hand-written `.erl` to carry
    `-behaviour(application)` and be the app entry point (needs a behaviour scan in
    the `.erl` branch to set `appMod`).
  - **`otp.Apply(module, func, args...)` marker:** direct synchronous
    Go→pure-native-function calls without a server wrapper (one new transpiler
    marker → `module:func(args)`).
  - **Inline escape hatch (option B):** embed raw Erlang for individual expressions
    inside a Go module (marker syntax + transpiler pass-through).
- **Integration-test `beam.smp` leak:** the shared `t.Cleanup` in
  `native_integration_test.go` and the pre-existing
  `selfcontained_integration_test.go` discard `bin/stop`'s error → leaked nodes on
  repeated local runs. A standalone follow-up touching both tests (assert
  `stop.Run()` or add a SIGKILL fallback) would fix it; not release-blocking.
- **relup/appup hot upgrades** — groundwork present; no `release_handler` upgrade
  flow, no appup generation.
- **`bin/attach`** for the target system — erts ships `to_erl`/`run_erl`;
  `bin/start` boots via `erl -detached`, so `to_erl` can't attach. Wire through
  `run_erl` (with a LOGDIR) or generate a `bin/attach` remsh wrapper.
- **cookie-on-argv residual (from 0.2.5):** short-lived control nodes in
  `wm stop`/`status`/`call`/`attach` still pass `-setcookie` on argv (sub-second
  exposure; long-lived node fixed in 0.2.5). Fold into the run-file mechanism.
- **shared command-preamble DRY** (`start`/`stop`/`status`/`call`/`attach`/
  `release`); `stop`/`status` missing `ValidateVersion`.
- **transpiler subset (0.2.2/0.2.3 deferrals):** `handle_cast`/`handle_info`/
  `terminate`/`code_change`; multiple state fields; `Init` args; multiple
  gen_servers per module; operators beyond `+`; supervisor strategy/child-spec
  selection; multiple/nested children; richer `.app`.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** — errors on anything outside
  it, by design. **0.2.7 does not touch the transpiler**; native constructs are
  handled by hand-written `.erl`, not the transpiler.
- **Native `.erl` module name = file basename** (erlc requires `-module(x)` ==
  `x.erl`); `wm build`/`wm release` reject a mismatch and an unsafe basename.
- **`wm build` does not erlc** (it only writes `.erl` + `.app`); `wm release`
  erlc's. The native `-module` cross-check matters most for `wm build`.
- **Integration tests leak `beam.smp` nodes** (see Build & test above).
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from
  `main`, in order origin→upstream→github. Copilot review gate runs before
  github-bound commits; findings are folded via re-squash before the push.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`).
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler / integration tests.

## Key artifacts

- 0.2.7 spec: `docs/superpowers/specs/2026-07-12-wintermute-0.2.7-native-erl-interop-design.md`
- 0.2.7 plan: `docs/superpowers/plans/2026-07-12-wintermute-0.2.7-native-erl-interop.md`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2}-wintermute-0.2.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.2.7 — no `pkg/` change)
- Verified sources + local build record: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
