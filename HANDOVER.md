# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.3 OTP application deployment is merged to `main` (`aebbf51`) and pushed to origin.**
It was the **fourth step of the 0.2.x line** (option A2, executed inline, 6 tasks TDD'd).
The completed 0.2.x line is promoted to **0.3.0** after a further Copilot review + fix
session. See the `release-versioning-model` memory.

**0.2.0** (hardening), **0.2.1** (distributed interop), **0.2.2** (gen_server), and
**0.2.3** (application deployment) are all merged to `main` and pushed to origin.
**0.1.0** remains shipped (`production-0.1.0`). Currently on `main`; no work branch open —
the next step starts a fresh `development-0.2.4`.

The echo interop ladder now proves interchangeability at **four** levels — single-node
(rungs 1–4), **distributed** (II.1–II.4), **gen_server** (III.1–III.4), and **OTP
application** (IV.1–IV.4, the echo as `application → supervisor → gen_server`, booted via
`application:start/1` with a generated `.app`). Transpiled Wintermute is interchangeable
with hand-written Erlang at all four.

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

### Verification gate (all green)

- `go build` + `go test ./...` green.
- **Real integration ladder:** all **16** rungs (1–4, II.1–II.4, III.1–III.4, IV.1–IV.4)
  PASS on real OTP 29.0.3.
- `govulncheck` / `gitleaks` clean. `gosec` now **11** findings (was 10) — the extra one is
  the same accepted dual-use class (path/perms in the CLI file-writing path), because
  `wm build` now also writes the `.app` and reads VERSION. No new vulnerability class.

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/   # all 16 rungs incl. IV.1–IV.4
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar` (GNU/BSD),
plus `libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Next step: start the 0.2.4 brainstorm

0.2.3 is merged; no work branch is open. The staged deployment plan (agreed during the
0.2.3 brainstorm) is:

- **0.2.4 = B — persistent node:** `wm` keeps a real node alive hosting the application
  (rather than boot-and-`init:stop`); `application:start` on a running node. Cross-node
  gen_server (`gen_server:call({global, echo}, …)`) folds in here. See the
  `otp-execution-model-direction` memory.
- **0.2.5 = C — full OTP release** (`releases/`, `sys.config`), **conditional** on A/B
  proving the structure holds.
- **Evaluate explicitly (own step): native-Erlang interop** — allow hand-written `.erl`
  for what Go can't express but OTP needs (records, macros, guards). See the
  `native-erlang-interop-open-question` memory. After the deployment foundation.

Start 0.2.4 like the prior steps: branch (`development-0.2.4-main`/`-work` from `main`,
`printf '0.2.4\n' > VERSION`), then `superpowers:brainstorming` → spec → plan → execute.

## 0.2.x backlog (deferred, per-task + final reviews)

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
  otpapp fixtures import each other but are only ever read as source by the transpiler.

## Key artifacts

- 0.2.3 spec: `docs/superpowers/specs/2026-07-11-wintermute-0.2.3-design.md`
- 0.2.3 plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.3.md`
- 0.2.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.2.0*`
- 0.1.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.1.0*`
- SDK index: `docs/SDK-INDEX.md` (regenerate: `/sdk-index`)
- Verified sources + local build record + pinned SHA-256: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
