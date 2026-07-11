# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.4 persistent node is feature-complete on `development-0.2.4-work` (HEAD
`979b90b`, 14/14 tasks TDD'd + a final-review critical fix), verification gate green.**
The whole-branch review (opus) caught a **critical bug the mocked unit tests and the
`startCmd`-bypassing ladder never covered**: `wm start` never emitted `<app>.app`, so
`application:start` failed silently on the detached node (a `-detached` boot exits 0
regardless), and — found via the fix's real-flow e2e test — `wm call` never
ping/sync-connected to the node so `{global, echo}` never resolved. Both fixed
(`8df00fe`) with a new **unmocked** integration test (`start_integration_test.go`,
`TestStartBootsAppEndToEnd`) that drives the real `start → status → call → stop` flow
and asserts the app actually runs + echoes `hello`; assertion tightened in `979b90b`.
Lesson: `run-real-toolchain-build-early` — green mocked tests + a ladder that hand-rolls
its own boot did not exercise the flagship CLI path.
Not yet merged to `main` — per the git workflow, the next step is the squash-merge
`-work` → `-main` → `main` (see "Next step" below), intentionally outside this task's scope.
**0.2.3 OTP application deployment remains released on `main` = `a1e441d`, tagged
`v0.2.3`,** fast-forward-pushed to ALL THREE remotes (`origin`, `upstream`, `github`).
The completed 0.2.x line is promoted to **0.3.0** after a further Copilot review + fix
session. See the `release-versioning-model` memory.

The gated remotes (`upstream`, `github`) had been stale at pre-0.2.0 (`bb72ef8`); the
0.2.3 push caught them up with the whole 0.2.0–0.2.3 line under tag `v0.2.3`. The
**Copilot review gate** run before that github push found and TDD-fixed two real bugs
(folded into the release, commit `9489ca5`): non-deterministic method-map iteration
(now sorted) and an otp-marker arity panic (now a positioned error). Fix branches
`fix-transpile-review-{main,work}` archived on origin.

**0.2.0** (hardening), **0.2.1** (distributed interop), **0.2.2** (gen_server), and
**0.2.3** (application deployment) are all on `main` and all three remotes.
**0.1.0** remains shipped (`production-0.1.0`, tag `v0.1.0`). **0.2.4** (persistent
node) is complete on `development-0.2.4-work`, pending merge.

The echo interop ladder now proves interchangeability at **five** levels — single-node
(rungs 1–4), **distributed** (II.1–II.4), **gen_server** (III.1–III.4), **OTP
application** (IV.1–IV.4, the echo as `application → supervisor → gen_server`, booted via
`application:start/1` with a generated `.app`), and **persistent node** (V.1–V.4, the
app kept alive on a detached node and called cross-node via `{global, echo}`).
Transpiled Wintermute is interchangeable with hand-written Erlang at all five.

### 0.2.3 delivered

- **Application + supervisor by convention:** a Go type with `Start`/`Stop` methods →
  `-behaviour(application)`; a Go type with `Init() []otp.Child` → `-behaviour(supervisor)`
  with a fixed `{one_for_one, 1, 5}` / `permanent, 5000, worker` child spec. New markers
  `otp.StartSupervisor` (→ `Sup:start_link()`) and `otp.Child{ID, Start}` (child spec;
  `Start` is a package-qualified func value → MFA `{mod, fn, []}`).
- **`.app` resource emission:** `transpile.Module` now returns a `Result{Erl, Module,
  Behaviour, Registered}`; `transpile.AppResource` builds the `.app`. `wm build` accepts
  **multiple** `.go` files and emits `<app>.app` when an application module is present
  (`vsn` from `--vsn` or the VERSION file; `registered` from `otp.StartServer` names;
  `modules` from the transpiled set). `transpile.File` retained as a thin wrapper.
- **Fixtures** (`testdata/otpapp/`) + **ladder rungs IV.1–IV.4** via new `runOtpApp` helper.

### 0.2.4 delivered

- **Persistent node, detached-first:** `wm start` transpiles + compiles the sources,
  emits the `<app>.app` resource (vsn from `--vsn`/VERSION, default `0.0.0`), and boots
  the OTP application on a real detached BEAM node (`erl -detached`,
  `application:start/1`) instead of boot-and-`init:stop`; the node keeps running after
  the CLI exits. `wm call` bounded-converges the node connection (`net_adm:ping` +
  `global:sync` + `global:whereis_name`) before the cross-node call, failing cleanly on
  timeout.
- **Five subcommands:** `wm start` (boot detached node, write State-File), `wm stop`
  (rpc `init:stop`, remove State-File), `wm status` (`net_adm:ping` reachability +
  `which_applications`), `wm call` (cross-node `gen_server:call({global, Name}, Req)`
  from a control node), `wm attach` (interactive `erl -remsh` to the running node;
  detaching from the shell leaves the node running).
- **State-File identity:** `NodeState{Node, Cookie, CodePath}` persisted under
  `~/.local/state/wintermute/` (owner-only, `0o600`/`0o700`), read by
  `stop`/`status`/`call`/`attach` to reconnect to the node `start` created. Missing
  State-File → actionable error on all four.
- **Global-registration markers:** `otp.StartServerGlobal` (→ `{global, Name}` instead
  of local registration) and `otp.CallGlobal` (→ `gen_server:call({global, Name}, Req)`),
  transpiled in Tasks 1–3, exercised end-to-end by the persistent-node ladder.
- **Testing seam:** `runErl`/`captureErl`/`attachErl` function-var seams in
  `internal/pkg/cli/node.go` make node lifecycle unit-testable without a real BEAM.
- **Separate fixtures:** `testdata/persistent/` (Go + hand-written Erlang) mirror
  `testdata/otpapp/` but use the global markers, keeping the two ladders independent.
- **Ladder rungs V.1–V.4:** cross-node persistent-node interchangeability (Erlang↔Erlang,
  Wintermute caller, Wintermute server, both Wintermute), via `runPersistent` with a
  bounded ping-poll (`net_adm:ping` + `global:sync` + `global:whereis_name`, up to
  30×100ms) to avoid racing global-name registration after a detached boot.
- **Scope decision:** the detached-node log file is deferred to the backlog (see below);
  `wm status` reporting `pang` + the node name is the 0.2.4 diagnosis surface.

### Verification gate (all green) — 0.2.4, run 2026-07-11

- `go build -o bin/wm ./cmd/wm` — clean.
- `go test ./...` — all packages green (`cli`, `erlang`, `transpile`, `pkg/otp`).
- **Real integration ladder:** `go test -tags integration ./internal/pkg/ladder/`
  — all **20** rungs PASS on real OTP 29.0.3 (`~/.local/erlang/29.0.3`): rungs 1–4,
  II.1–4, III.1–4, IV.1–4, and the four new **V.1–V.4** (persistent node).
- **Real CLI e2e:** `go test -tags integration ./internal/pkg/cli/ -run TestStartBootsAppEndToEnd`
  — drives the unmocked `wm start → status → call → stop` flow on real OTP; asserts
  `<app>.app` on disk, `{echoapp,…}` in `which_applications`, and `wm call echo hello`
  → `hello`. This is the regression guard for the critical fix (`8df00fe`).
- `govulncheck ./...` — no vulnerabilities found.
- `gitleaks detect` — no leaks found (127 commits scanned).
- `gosec ./...` — **17** findings (was 11 in 0.2.3), **all in the previously-accepted
  dual-use classes**, no new vulnerability category:
  - **+5 genuinely new (in `internal/pkg/cli/`, all new in 0.2.4):** `G204` findings
    (subprocess launched with variable — `captureErl`/`attachErl` spawning `erl`, plus
    the `start`/`call` control-node `erl` invocations, same class as the pre-existing
    `execRunner`/`tar` `G204` findings) + 2×`G304` (file inclusion via variable —
    State-File read/write, same class as the pre-existing `os.ReadFile(srcPath)` `G304`
    findings). The State-File write itself already uses `0o600`/dir `0o700` — no
    `G306`/`G301` fired there, confirming the owner-only-permissions fix (Task 4) holds.
  - **+1 on unchanged pre-0.2.4 code:** `G703` (CWE-22, "path traversal via taint
    analysis", HIGH) at `cli.go:217`, inside `wm run` — a line unchanged since
    `d3a544c2`/`83ebc7a9` (0.1.0/0.2.0). This is gosec's taint-mode variant of the
    already-accepted `G304` class; it's the same underlying "user-supplied output path
    in a local CLI" risk, evidently now reachable in the taint graph because 0.2.4 added
    more argument-derived taint sources into the shared `cli` package. Not a new class,
    not a new code path — triaged as accepted alongside the rest.
  - No `semgrep` run this cycle (not required by the gate; core four tools only).

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/   # all 20 rungs incl. IV.1–IV.4, V.1–V.4
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar` (GNU/BSD),
plus `libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Next step: merge 0.2.4, then start the 0.2.5 brainstorm

**Merge first (per the git workflow):** `development-0.2.4-work` → squash-merge into
`development-0.2.4-main` (`git merge --squash` + `git commit -s`) → regular merge into
`main` → create `production-0.2.4`. This is deliberately **not** done as part of Task 14
— docs + verification gate were the last plan task; the merge itself is a separate,
explicit step so the verification-gate evidence in this file is reviewable against the
still-open work branch before it lands.

The staged deployment plan (agreed during the 0.2.3 brainstorm; B now done) is:

- **0.2.4 = B — persistent node. DONE** (this file, "0.2.4 delivered" above):
  `wm` keeps a real node alive hosting the application (rather than boot-and-
  `init:stop`); cross-node gen_server (`gen_server:call({global, echo}, …)`) works.
  See the `otp-execution-model-direction` memory.
- **0.2.5 = C — full OTP release** (`releases/`, `sys.config`), **conditional** on
  0.2.4 holding after merge (Copilot review gate + real-build re-check, per the
  `run-real-toolchain-build-early` memory).
- **Evaluate explicitly (own step): native-Erlang interop** — allow hand-written `.erl`
  for what Go can't express but OTP needs (records, macros, guards). See the
  `native-erlang-interop-open-question` memory. After the deployment foundation.

Start 0.2.5 like the prior steps: merge 0.2.4 to `main` first, then branch
(`development-0.2.5-main`/`-work` from `main`, `printf '0.2.5\n' > VERSION`), then
`superpowers:brainstorming` → spec → plan → execute.

## 0.2.x backlog (deferred, per-task + final reviews)

- **persistent node (0.2.4 deferrals):** fixed control-node names — `ctrlNode()`
  (`wmctrl@127.0.0.1`, Task 8, reused by 9/10/11) and `wm attach`'s
  `wmattach@127.0.0.1` (Task 11) are hardcoded, not unique per invocation; two
  concurrent `wm` invocations against the same host would clash on epmd. Make them
  unique (random suffix / PID), matching the ladder's own `idx`+PID node-naming scheme
  (Task 13). `wm ls` (list running Wintermute-managed nodes) — no discovery command
  exists yet, only `wm status` against the current State-File. `-heart` restart — the
  detached node has no supervision if the BEAM itself dies; `erl -heart` was not wired
  in. **Detached-node log file** — deferred from this cycle (Task 14 Step 3 scope
  decision above): the OTP 29 kernel-logger flag string is fragile for the echo subset;
  `wm status` reporting `pang` + node name is the 0.2.4 diagnosis surface. **Log
  rotation** — once a log file exists, needs a rotation story. **Shared-preamble DRY
  consolidation** — `start`/`stop`/`status`/`call`/`attach` each re-derive
  layout/State-File/control-node setup inline; a single preamble helper would remove
  the duplication (Copilot-review-shaped cleanup, not yet done). **`stop`/`status`
  missing `ValidateVersion`** — `wm start` validates the OTP version via
  `erlang.ValidateVersion` before booting (Task 7); `wm stop`/`wm status` skip that
  check and rely on the State-File alone, so a version mismatch surfaces later/less
  clearly than in `start`. **`strings.CutSuffix` nit** — a string-trimming spot uses
  manual slicing where `strings.CutSuffix` (stdlib, Go 1.20+) would be more idiomatic;
  cosmetic, deferred. **Cookie on argv (SECURITY — document before 0.3.0 promotion):**
  `wm start` passes the RCE-grade Erlang cookie via `erl -setcookie <cookie>` on the
  detached node's command line, so it lives in `/proc/<pid>/cmdline` for the node's whole
  lifetime — any local user on a multi-user host can read it (`ps`) and connect + execute
  code as the node owner, undercutting the State-File's `0o600` intent. Matches stock
  `erl -setcookie` behaviour and is acceptable for the single-user dev-host threat model,
  but must be documented as a known limitation (or moved to `~/.erlang.cookie` `0o400` /
  an env-passed cookie file) before the line is promoted to the gated `github`/`upstream`
  remotes. **`wm stop` honesty:** success is the control-node exit code only; `rpc:call`
  returning `{badrpc, …}` (node already dead) still exits 0, so the State-File is removed
  regardless — desired for "already dead", but does not distinguish a clean stop from a
  no-op. **Eval interpolation not escaped:** node name (`stop`/`status`) and gen_server
  `name` (`call`) are interpolated raw into the control-node `-eval`; a crafted
  `--name`/`<name>` injects Erlang into the user's own short-lived control node
  (self-inflicted, not privilege escalation) — worth a guard. **Ladder/e2e robustness:**
  `runPersistent` folds stderr into the `== "hello"` assertion (fail-closed,
  false-negative only); no `exec`/context timeout on the caller; hardcoded `vsn "0.2.4"`
  in `transpilePersistentApp`.
- **application/supervisor (0.2.3 deferrals):** supervisor strategy/intensity/restart/
  shutdown/type selection (hardcoded `one_for_one,1,5` / `permanent,5000,worker`); multiple
  children; nested supervisors; richer `.app` (deps beyond kernel/stdlib, `env`,
  `start_phases`); application `stop/1` body (always `ok`).
- **gen_server (0.2.2 deferrals):** `handle_cast`/`handle_info`/`terminate`/`code_change`;
  multiple state fields; `Init` arguments; multiple gen_server instances per module;
  operators beyond `+`.
- **Embedded/anonymous struct fields** bypass the field-casing guard (`fld.Names` empty)
  and would silent-drop in composite literals — reject `len(fld.Names)==0` explicitly.
- `errorf` calls `em.fset.Position` unconditionally — a white-box `emitter{}` on an error
  path would panic (safe today; add a nil-fset guard when white-box error tests appear).
- B6 `tar --version` probe runs *after* the 64 MiB download — reorder before it.
- **DRY (Copilot 0.2.3 review):** `parseVsnFlag`/`parseVersionFlag`/`parseOutFlag` in
  `cli.go` are near-identical `--name X | --name=X` extractors — factor into one
  `parseStringFlag(args, flag, default)` helper (parseVersionFlag keeps its default +
  validation). Pure refactor, no behaviour change; deferred from the pre-push fix.
- Cosmetics: single atom-collision test + "duplicate clause" wording; nullary-call roadmap
  message lacks a substring test; multi-line `pat` reindent assumption; size-cap error
  lacks URL context.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** (no `go/types`). It errors on anything
  outside that subset — by design (never silent-wrong), with `file:line:col` positions.
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from `main`.
  Develop on `origin`. Copilot review gate runs before github-bound commits (at promotion).
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored scratch.
- `testdata/` Go fixtures are not built by `go test ./...` (Go ignores `testdata/`); the
  otpapp and persistent fixtures import each other but are only ever read as source by
  the transpiler.

## Key artifacts

- 0.2.4 spec: `docs/superpowers/specs/2026-07-11-wintermute-0.2.4-design.md`
- 0.2.4 plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.4.md`
- 0.2.3 spec: `docs/superpowers/specs/2026-07-11-wintermute-0.2.3-design.md`
- 0.2.3 plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.3.md`
- 0.2.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.2.0*`
- 0.1.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.1.0*`
- SDK index: `docs/SDK-INDEX.md` (regenerate: `/sdk-index`)
- Verified sources + local build record + pinned SHA-256: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
