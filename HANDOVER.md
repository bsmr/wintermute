# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-12.

## Where things stand

**0.2.6 self-contained OTP target system is RELEASED: squash-merged to `main`
= `308cb51`, tagged `v0.2.6`, pushed to ALL THREE remotes (`origin`, `upstream`,
`github`); `production-0.2.6` created.** It is the seventh step of the 0.2.x line
(single-node → distributed → gen_server → application → persistent node → full OTP
release → **self-contained target system**), and the second with no transpiler
change — pure CLI/release tooling. The 7-task build was TDD'd: Tasks 1–5
subagent-driven (fresh implementer + two-stage review per task), Tasks 6–7
(real-OTP integration + gate) done inline. Two review gates fired three real fixes
this cycle: the final whole-branch review (opus) caught a tar-bomb (artifact now
unpacks into a single `<app>-<vsn>/` dir); the Copilot gate caught a shell-injection
via `--vsn` into the generated launchers and a swallowed `TarGz` close error — all
folded in with regression tests (see below).

**With 0.2.6 the 0.2.x deployment line is feature-complete for the echo subset.**
0.2.5 (`v0.2.5`, `538bae0`), 0.2.0–0.2.4, and 0.1.0 (`production-0.1.0`, `v0.1.0`)
are all released on `main` and all remotes.

The completed 0.2.x line is promoted to **0.3.0** after a further review + fix
session (see the `release-versioning-model` memory).

### 0.2.6 delivered

- **`wm release --self-contained`** produces a standalone OTP **target system**
  tarball (`<app>-<vsn>.tar.gz`) that Ops unpacks on a host with **no Erlang
  installed** and starts via `./bin/start`. No `wm`, no system Erlang, no secret
  in the artifact.
- **How it is built** (extends the 0.2.5 release builder; `--self-contained`
  implies `--tar`): the boot script is generated **non-local** (paths resolve from
  the bundled `$ROOTDIR/lib` at boot — the critical difference from 0.2.5's `local`
  boot), `sasl` is added to the `.rel`, and `systools:make_tar` bundles the ERTS
  (`{erts, <OtpRoot>}`). The make_tar output is then assembled into a full target
  system in Go: unpack → generate `start_clean.boot` (`systools:make_script`) +
  `releases/RELEASES` (`release_handler:create_RELEASES`) → write top-level `bin/`
  (erts launchers copied from `$OTP/bin` + generated self-locating `bin/start`/
  `bin/stop`) + `releases/start_erl.data` → repack.
- **Relocatability is free:** modern erts launchers self-locate
  (`ROOTDIR=$(find_rootdir "$0" …)`), so no ROOTDIR rewriting; `bin/start` is a
  generated self-locating wrapper that boots the bundled erts directly.
- **Cookie via OTP-default `~/.erlang.cookie`:** `bin/start` sets no `-setcookie`;
  erl auto-creates `~/.erlang.cookie` (`0o400`) on first run. No cookie in the
  tarball, none on argv; same-host app/control nodes share it automatically.
- **New code:** `internal/pkg/release/{archive.go (Untar/TarGz, mode-preserving,
  traversal-guarded), target.go (WriteLauncherLayout)}` + text helpers
  (`StartScript`/`StopScript`/`StartErlData`) in `release.go`;
  `assembleTargetSystem` in `internal/pkg/cli/release.go`. No transpiler change;
  **`wm start` unchanged** (still boots the 0.2.5 metadata release against local OTP).
- **Ladder rung VII** (`TestSelfContainedTargetSystemEndToEnd`): the real
  `wm release --self-contained` artifact, unpacked and booted via `bin/start`
  under a **fully scrubbed environment** (`env` with only `PATH=/usr/bin:/bin` +
  `HOME`, no system Erlang), with a scrubbed control node resolving `{global, echo}`
  → `hello`.

### Real-OTP-surfaced fixes (Task 6 — vindicates run-real-toolchain-build-early)

The unit tests (mocked make_tar output) passed, but the first real
`wm release --self-contained` failed — two assembly assumptions were wrong, fixed
in `d2710b2` and guarded by rung VII:
- **`systools:make_tar` names the release boot `start.boot` inside the bundle**
  (not `<app>.boot`); `WriteLauncherLayout` now only synthesises `start.boot` when
  absent.
- **`make_tar` does not bundle `vm.args`** (and only conditionally `sys.config`);
  the assembly now copies both from the built release into the bundle so
  `bin/start` finds them.
- Bonus finding: this OTP build's erts bundle contains **no symlinks**, so the
  `Untar` symlink-handling concern flagged in review is moot in practice.

### Verification gate (all green) — 0.2.6, run 2026-07-12

- `go build -o bin/wm ./cmd/wm` — clean.
- `go test ./...` — all packages green (`cli`, `erlang`, `release`, `transpile`, `pkg/otp`).
- **Real integration ladder:** `go test -tags integration ./internal/pkg/ladder/`
  — all **23** rungs PASS on OTP 29.0.3 (I–IV, V.1–4, VI.1/VI.2 + tarball).
- **Real CLI integration:** `go test -tags integration ./internal/pkg/cli/` — the
  0.2.5 two-step e2e **and** the new **rung VII** self-contained scrubbed-boot e2e,
  both green.
- `govulncheck ./...` — no vulnerabilities found.
- `gitleaks detect` — no leaks found.
- `gosec ./...` — **48** findings (was 24 in 0.2.5). The delta is the file/archive-
  heavy target-system assembly + `archive.go`, **all operating on wm's own freshly-
  built artifacts**. **The only HIGH findings are 4×`G703`** (path-traversal via
  taint) — the same accepted dual-use class as 0.2.4/0.2.5 (gosec's taint variant of
  `G304`: a user-supplied path in a local CLI). The rest are the accepted
  `G204`/`G301`/`G304`/`G306` (subprocess/dir-perms/file-inclusion/file-perms on
  distributable release artifacts) + `G104` (LOW, deferred-Close/`_ = os.Symlink`).
  **Three genuinely-new HIGH/MEDIUM findings were addressed, not blanket-accepted**
  (`2c7ff8c`): `G115` fixed (`hdr.FileInfo().Mode().Perm()`), `G110`/`G122`
  `#nosec`-annotated with rationale (Untar/TarGz consume wm's own make_tar output /
  own work dir, never untrusted input; the traversal guard stays). No new
  vulnerability category reaches an *unaccepted* HIGH/CRITICAL.

### Copilot review gate (pre-github push) — findings folded in

Run on the staged squash diff (per `CLAUDE.md`). It found two real defects, both
fixed (`4af8ba8`) and folded into the release:
- **Critical:** `vsn` is spliced raw into the generated `/bin/sh` launchers
  (`StartScript`/`StopScript`), and `validAppName` only blocks path separators — a
  `--vsn '$(touch x)'` (or a poisoned VERSION file) produced a `bin/start` that ran
  arbitrary code on the **target** host when Ops ran it. New `validVsn`
  (`^[A-Za-z0-9._-]+$`) rejects shell metacharacters; `releaseCmd` validates `vsn`
  with it before any use (`TestReleaseRejectsShellMetacharVsn`). `node` was already
  safe via `validNodeName`.
- **Medium:** `TarGz` discarded its deferred `gz`/`tw` Close errors, so a flush
  failure (e.g. full disk) yielded a silently truncated release tarball reported as
  success; now surfaced via a named return (`TestTarGzSurfacesWriteError`).

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/     # 23 rungs
go test -tags integration ./internal/pkg/cli/        # 0.2.5 e2e + rung VII self-contained
# Self-contained artifact by hand:
./bin/wm release <sources>... --out DIR --self-contained   # -> DIR/<app>-<vsn>.tar.gz
```

## Next step: pick the next milestone (0.2.6 is shipped)

0.2.6 is merged, tagged `v0.2.6`, and on all three remotes — nothing pending.
The staged deployment plan (agreed during the 0.2.3 brainstorm) is now fully
delivered: B (persistent node, 0.2.4), C (full OTP release, 0.2.5), and the ERTS-
bundling follow-up (self-contained target system, 0.2.6). Candidates for next
(no decision made yet — start with `superpowers:brainstorming` once chosen):

- **0.3.0 promotion** of the whole 0.2.x line (the line is feature-complete for the
  echo subset — single-node → distributed → gen_server → application → persistent
  node → release → self-contained target system).
- **Native-Erlang interop** — hand-written `.erl` for what Go can't express
  (records, macros, guards). See the `native-erlang-interop-open-question` memory.
- **relup/appup hot upgrades** — 0.2.6 laid the groundwork (`RELEASES`,
  `start_erl.data`); the upgrade flow itself is unbuilt.

## 0.2.x backlog (deferred)

- **relup/appup hot upgrades** — groundwork present (`RELEASES`/`start_erl.data`);
  no `release_handler` upgrade flow, no appup generation.
- **`bin/attach`** for the target system — the erts ships `to_erl`/`run_erl`;
  `bin/start` currently boots via `erl -detached` (not `run_erl`), so `to_erl`
  cannot attach. Wire `bin/start` through `run_erl` (with a LOGDIR) if attach is
  wanted, or generate a `bin/attach` remsh wrapper.
- **Cross-host cookie distribution** for the target system — Ops concern (standard
  Erlang; same-host is automatic via `~/.erlang.cookie`).
- **`Untar`/`TarGz` robustness** (currently fine — inputs are self-produced): no
  decompression limit (`G110`, accepted); `Untar` `TypeSymlink` branch does not
  `MkdirAll` the parent and swallows the error (moot — no symlinks in this OTP's
  bundle); `TarGz` discards deferred `gz`/`tw` Close errors.
- **cookie-on-argv residual (from 0.2.5, still open):** the short-lived control
  nodes in `wm stop`/`status`/`call`/`attach` still pass `-setcookie` on argv
  (sub-second exposure; the long-lived node was fixed in 0.2.5). Fold into the
  run-file mechanism before 0.3.0. The `wm start` cookie run-file `WriteFile` could
  use `O_EXCL` for an unconditional `0o600` (not exploitable — parent dir `0o700`;
  0.2.5 added an explicit `Chmod`).
- **shared command-preamble DRY** (`start`/`stop`/`status`/`call`/`attach`, now also
  `release`); `stop`/`status` missing `ValidateVersion`; `strings.CutSuffix` nit in
  cli.go; `absEbin` unescaped in the make_script/make_tar `-eval` (self-inflicted
  via `--out`, no trust boundary).
- **transpiler subset (0.2.2/0.2.3 deferrals):** `handle_cast`/`handle_info`/
  `terminate`/`code_change` (the e2e's benign erlc warning); multiple state fields;
  `Init` args; multiple gen_servers per module; operators beyond `+`; supervisor
  strategy/child-spec selection; multiple/nested children; richer `.app`.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** — errors on anything outside it,
  by design. **0.2.6 does not touch the transpiler.**
- **`wm start` takes a release dir** (0.2.5), not sources; the 0.2.6 self-contained
  artifact is run via its own `./bin/start`, NOT `wm start`.
- **`--self-contained` needs a NON-local boot;** never make the 0.2.5 metadata-
  release path non-local (`wm start` boots it against the system OTP, whose
  `$ROOTDIR/lib` has no `<app>` — it relies on the `local` boot's absolute app-ebin
  path).
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from `main`,
  in order origin→upstream→github. Copilot review gate runs before github-bound
  commits.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`) — a
  stale brief once leaked between versions.
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler.

## Key artifacts

- 0.2.6 spec: `docs/superpowers/specs/2026-07-12-wintermute-0.2.6-design.md`
- 0.2.6 plan: `docs/superpowers/plans/2026-07-12-wintermute-0.2.6.md`
- 0.2.5 spec/plan: `docs/superpowers/{specs,plans}/2026-07-11-wintermute-0.2.5*`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1}-wintermute-0.2.*`
- SDK index: `docs/SDK-INDEX.md` (regenerate: `/sdk-index`; unchanged in 0.2.6 —
  no `pkg/` change)
- Verified sources + local build record + pinned SHA-256: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
