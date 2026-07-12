# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.5 full OTP release is COMPLETE on `development-0.2.5-work`, pending merge.**
It is the sixth step of the 0.2.x line (single-node → distributed → gen_server →
application → persistent node → **full OTP release**). It is the **first 0.2.x step
with no transpiler change** — pure CLI/release tooling. The 11-task build was
TDD'd task-by-task: Tasks 1–8 subagent-driven (fresh implementer + two-stage
review per task), Tasks 9–11 (real-OTP integration) done inline by the controller.

**0.2.4 persistent node remains released on `main` = `049e19a`, tagged `v0.2.4`,**
pushed to ALL THREE remotes (`origin`, `upstream`, `github`). **0.2.0–0.2.3** are
all on `main` and all remotes; **0.1.0** shipped (`production-0.1.0`, tag `v0.1.0`).

The completed 0.2.x line is promoted to **0.3.0** after a further review + fix
session (see the `release-versioning-model` memory).

### 0.2.5 delivered

- **Two-step release flow (the Erlang way):** `wm release <sources>` builds a
  formal OTP release; `wm start <release-dir>` boots the *finished* release. This
  is a **breaking change** to `wm start` (was `wm start <go-sources>` in 0.2.4) —
  in OTP one never starts source, always a built artifact (like
  `rebar3 release && bin/echo start`). Only consumers were README + the project's
  own tests, both updated.
- **`wm release`** transpiles + compiles into a `lib/<app>-<vsn>/ebin` layout,
  emits `<app>.rel` / `sys.config` / `vm.args` / `wm.json`, and generates the boot
  script via `systools:make_script` (`local` option, `{path,[ebin]}` — kernel/
  stdlib resolve from the running OTP's default code path). `--tar` additionally
  packages a release tarball via `systools:make_tar` (**no bundled ERTS** — runs
  against an installed same-version OTP).
- **`wm start <dir>`** reads `wm.json` (app/vsn/node), boots
  `erl -detached -boot .../releases/<vsn>/<app> -config sys.config -args_file
  <release vm.args> -args_file <run vmargs>` — the release boot script starts
  kernel+stdlib+app itself, **no `-eval application:start`**. `stop`/`status`/
  `call`/`attach` unchanged (State-File driven).
- **Cookie off argv — closes a 0.2.4 security backlog item.** The release
  `vm.args` carries `-name` only, **no cookie** (tarball stays secret-free).
  `wm start` generates a fresh cookie at boot, writes it to a `0o600` run-file
  under the state dir, and supplies it via a second `-args_file`. The cookie never
  appears on argv (`ps`/`/proc/<pid>/cmdline`) and never enters the release.
- **`sys.config`** is an empty-but-valid scaffold `[{<app>, []}].`, actually
  loaded via `-config` to prove the wiring. No Go application-env marker in 0.2.5
  (deferred).
- **New package `internal/pkg/release/`** (pure, TDD'd): `RelResource`,
  `SysConfig`, `VmArgs`, `Manifest`/`ParseManifest`. OTP version discovery on
  `erlang.Layout`: `OtpLib()`, `ErtsVersion()`, `AppVersion(name)` (glob
  `Root/lib/erlang/{erts-*,lib/<name>-*}`).
- **`wm.json` manifest** is the single source of truth `wm start` reads (chosen
  over parsing `vm.args` / globbing `releases/*`).
- **Ladder rungs VI** (release-level interchangeability): VI.1 hand-written-Erlang
  release, VI.2 Wintermute-transpiled release — both boot via `erl -boot` and
  answer the same cross-node `{global, echo}` call `hello`. Plus a tarball check
  (make_tar → untar via stdlib → assert payload → boot). Honest scope: **two
  release rungs**, not four (release packaging has no caller/server cross-product).

### Security fix caught by the review gate (Task 7)

The whole-task review found a **CRITICAL** path-traversal in the first `wm start`
cut: `m.App`/`m.Vsn` from the untrusted `wm.json` were spliced into `filepath.Join`
+ `os.WriteFile` (the cookie run-file) **unvalidated**, so a crafted `wm.json`
(`App:"../../../../tmp/pwn"`) wrote the cookie file outside the state dir before
any check ran. The RED phase proved the exploit real. Fixed (`6a46d56`):
`validAppName(m.App)` + `validAppName(m.Vsn)` immediately after `ParseManifest`,
before any path is built or file written, with `TestStartRejectsTraversalAppName`/
`TestStartRejectsTraversalVsn` regression tests. This is why `wm start`'s node-name
guard (0.2.4) alone was insufficient — the manifest carries three attacker-
controllable fields, not one.

### Verification gate (all green) — 0.2.5, run 2026-07-11

- `go build -o bin/wm ./cmd/wm` — clean.
- `go test ./...` — all packages green (`cli`, `erlang`, **`release`** (new),
  `transpile`, `pkg/otp`).
- **Real integration ladder:** `go test -tags integration ./internal/pkg/ladder/`
  — all **23** rungs PASS on real OTP 29.0.3 (`~/.local/erlang/29.0.3`): rungs 1–4,
  II.1–4, III.1–4, IV.1–4, V.1–4, and the three new **VI.1/VI.2 + tarball**.
- **Real CLI e2e:** `go test -tags integration ./internal/pkg/cli/` —
  `TestReleaseStartCallStopEndToEnd` drives the unmocked `wm release → wm start
  <dir> → status → call → stop` flow on real OTP; asserts `wm.json`+`echoapp.boot`
  on disk, `{echoapp,…}` in `which_applications`, and `wm call echo hello` →
  `hello`. (Benign erlc warning `handle_cast/2 undefined` — echoserver fixture uses
  the 0.2.2 gen_server subset; deferred callbacks, not a regression.)
- `govulncheck ./...` — no vulnerabilities found.
- `gitleaks detect` — no leaks found (146 commits scanned).
- `gosec ./...` — **24** findings (was 17 in 0.2.4), **+7 all in `release.go`**,
  **all in the previously-accepted dual-use classes**, no new vulnerability class:
  3×`G301` (dir `0o755`) + 4×`G306` (file `0o644`) on the **distributable release
  artifacts** (`.rel`/`sys.config`/`vm.args`/`wm.json` and the lib/releases dirs) —
  non-secret, world-readable by design. The **only secret, the cookie run-file, is
  `0o600` and is NOT flagged** (satisfies G306) — the intended posture. No
  `semgrep` this cycle (core four only).

### Copilot review gate (pre-github push) — findings folded in

Run before the github-bound push (per `CLAUDE.md`), on the staged squash diff. It
found two real defects, both fixed (`6d012e9`) and folded into the release:
- **High:** `wm release` spliced `vsn` (from `--vsn` or a **poisoned VERSION file**)
  into filesystem paths **unvalidated** — the write-side counterpart to `startCmd`'s
  `validAppName(m.Vsn)`. Now `vsn` and `appMod` are guarded as safe path segments
  before any path is built (`TestReleaseRejectsTraversalVsn`).
- **Medium:** the cookie run-file kept a pre-existing file's looser perms
  (`WriteFile` mode applies only on creation); `os.Chmod` now forces `0o600`
  unconditionally (`TestStartRunFileForced0600`).
- **Low (backlog):** the `systools:make_script`/`make_tar` `-eval` strings splice
  `absEbin` (derived from the user's own `--out`) into a `["%s"]` Erlang string
  unescaped — a `"` in `--out` breaks out. `appMod` is now guarded; `absEbin` comes
  from the invoking user's own flag (no trust boundary), so it is a robustness nit,
  not a vuln — escape or reject `"` in `--out` if hardened later.

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3
go test -tags integration ./internal/pkg/ladder/   # all 23 rungs incl VI
go test -tags integration ./internal/pkg/cli/      # real wm release->start e2e
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
OTP tree lives under `Root/lib/erlang/` (`erts-17.0.3`, `lib/kernel-11.0.3`,
`lib/stdlib-8.0.2`) — this is what the version-discovery globs read.

## Next step: merge 0.2.5, then 0.2.6 (or 0.3.0 promotion)

**Merge first (per the git workflow):** `development-0.2.5-work` → squash-merge
into `development-0.2.5-main` (`git merge --squash` + `git commit -s`) → regular
merge into `main` → create `production-0.2.5`. Deliberately not part of Task 11 —
the verification-gate evidence in this file should be reviewable against the
still-open work branch before it lands.

The staged deployment plan (agreed during the 0.2.3 brainstorm; B and C now done):

- **0.2.5 = C — full OTP release. DONE** (this file).
- **0.2.6 = ERTS bundling / self-contained tarball** — `systools:make_tar` with
  `{erts, ErtsDir}` → a tarball that runs on a host with NO Erlang installed.
  Deferred from 0.2.5 because the "runs without Erlang" verification needs an
  erlang-free environment, hard to stage cleanly on this dev host.
- **Evaluate explicitly (own step): native-Erlang interop** — hand-written `.erl`
  for what Go can't express (records, macros, guards). See the
  `native-erlang-interop-open-question` memory.
- **0.3.0 promotion** of the whole 0.2.x line after a Copilot review + fix session.

Start the next step like the prior ones: branch (`development-0.2.6-main`/`-work`
from `main` after the 0.2.5 merge, bump VERSION), then
`superpowers:brainstorming` → spec → plan → execute.

## 0.2.x backlog (deferred)

- **cookie-on-argv — RESOLVED in 0.2.5 for the long-lived node** (cookie now in a
  `0o600` `-args_file` run-file, off argv and out of the release/tarball). Was the
  one item flagged fix-before-0.3.0-promotion. **Residual (final-review Minor):** the
  short-lived control nodes in `stop`/`status`/`call`/`attach` still pass
  `-setcookie st.Cookie` on argv (sub-second exposure per invocation vs. the node's
  whole lifetime — the real risk was the long-lived node). Fold these into the same
  run-file mechanism before 0.3.0 promotion. **Run-file nicety:** `os.WriteFile(...,
  0o600)` doesn't chmod a pre-existing file; `O_CREATE|O_EXCL` (or explicit chmod)
  would make the `0o600` guarantee unconditional (not exploitable — parent state dir
  is `0o700` owner-only).
- **release (0.2.5 deferrals):** **ERTS bundling / self-contained tarball → 0.2.6**
  (own step above). **Marker-driven `sys.config` env** (a Go `otp.Env{...}` marker)
  → backlog. **relup/appup** (hot code upgrades, `release_handler`, `RELEASES`) →
  far out. **make_script kernel/stdlib path** — currently `{path,[appEbin]}` only;
  kernel/stdlib resolve from the running OTP's default code path (works on this
  host); if a future target lacks them on the default path, add their ebin dirs
  (derivable via `Layout.AppVersion`) — a Task-5 implementer note, not needed yet.
  **`out`-shadowing nit** in `release.go`'s make_script block (captured bytes named
  `out`, shadows the release output dir — harmless; make_tar block already uses
  `res`). **README app-name** examples use `echo` (implies the app module is
  `package echo`, consistent with the 0.2.5 line).
- **persistent node (0.2.4 deferrals, still open):** fixed control-node names
  (`ctrlNode()` `wmctrl@127.0.0.1`, `wm attach` `wmattach@127.0.0.1`) not unique
  per invocation → epmd clash for concurrent invocations; `wm ls` (discovery)
  missing; `-heart` restart not wired; detached-node log file deferred (`wm status`
  `pang` + node name is the diagnosis surface); log rotation once a log exists;
  **shared-preamble DRY** (`start`/`stop`/`status`/`call`/`attach` re-derive
  layout/State-File/control-node inline; now also `release`); `stop`/`status`
  missing `ValidateVersion`; `strings.CutSuffix` nit (cli.go, still open);
  `wm stop` honesty (`{badrpc,…}` on a dead node still exits 0, State-File removed
  regardless).
- **transpiler subset (0.2.2/0.2.3 deferrals):** `handle_cast`/`handle_info`/
  `terminate`/`code_change` (the e2e's erlc warning); multiple state fields; `Init`
  args; multiple gen_server instances per module; operators beyond `+`; supervisor
  strategy/intensity/child-spec selection (hardcoded `one_for_one,1,5` /
  `permanent,5000,worker`); multiple/nested children; richer `.app`.
- **misc (older):** embedded/anonymous struct fields bypass field-casing guard;
  `errorf` unconditional `fset.Position`; B6 `tar --version` after the download;
  `parseVsnFlag`/`parseVersionFlag`/`parseOutFlag` near-duplicate extractors →
  one `parseStringFlag`-style helper; `globVersion` prefix unescaped into
  `filepath.Glob` (harmless — callers pass literal `kernel`/`stdlib`).

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** — errors on anything outside
  it, by design (never silent-wrong), with `file:line:col` positions. **0.2.5 does
  not touch the transpiler.**
- **`wm start` now takes a release dir**, not Go sources. Build with `wm release`
  first. The old sources-based `wm start` is gone.
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from
  `main`. Develop on `origin`. Copilot review gate runs before github-bound commits
  (at promotion).
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **SDD lesson:** always regenerate `task-N-brief` fresh (`scripts/
  task-brief`) — a stale 0.2.4 Task-7 brief leaked into the 0.2.5 Task-7 dispatch;
  the implementer caught it, but regenerate to be safe.
- `testdata/` Go fixtures are not built by `go test ./...` (Go ignores `testdata/`);
  the otpapp and persistent fixtures are only ever read as source by the transpiler.

## Key artifacts

- 0.2.5 spec: `docs/superpowers/specs/2026-07-11-wintermute-0.2.5-design.md`
- 0.2.5 plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.5.md`
- 0.2.4 spec/plan: `docs/superpowers/{specs,plans}/2026-07-11-wintermute-0.2.4*`
- 0.2.3 spec/plan: `docs/superpowers/{specs,plans}/2026-07-11-wintermute-0.2.3*`
- 0.2.0/0.1.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.{1,2}.0*`
- SDK index: `docs/SDK-INDEX.md` (regenerate: `/sdk-index`)
- Verified sources + local build record + pinned SHA-256: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
