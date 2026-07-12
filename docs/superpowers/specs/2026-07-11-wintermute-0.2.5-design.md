# Wintermute 0.2.5 — full OTP release (design)

**Date:** 2026-07-11
**Line:** 0.2.x deployment ladder, step C (after B = 0.2.4 persistent node)
**Module:** `go.muehmer.eu/wintermute`

## Goal

Package the transpiled OTP application (0.2.3/0.2.4) as a **formal OTP release**
and boot it from a `systools`-generated boot script instead of the ad-hoc
`erl -detached -eval "application:start(app)"` of 0.2.4. This is the standard,
deployable Erlang artifact: a `releases/<vsn>/` tree with `.rel`, boot script,
`sys.config`, and `vm.args`, plus a `lib/<app>-<vsn>/ebin/` code layout.

**0.2.5 is the first 0.2.x step with no transpiler change** — pure CLI/release
tooling. The go/ast → Erlang compiler is untouched.

## Decisions (brainstorm, 2026-07-11)

1. **Release ↔ start:** `wm start` boots **from** a built release
   (`erl -boot <release>` — the release boot script starts the apps itself). The
   0.2.4 ad-hoc `-eval application:start` boot is replaced.
2. **Release scope:** metadata release **+ tarball without bundled ERTS**
   (`systools:make_tar`, runs against an installed OTP of the same version). Fully
   verifiable on the dev host. ERTS bundling / self-contained tarball → 0.2.6.
3. **Two-step UX:** `wm release <sources>` builds the release; `wm start <dir>`
   boots a **finished** release. Explicit, ops-like separation.
4. **`wm start` interface (the Erlang way):** the verb acts on a **built artifact**,
   not on Go sources — like `rebar3 release && bin/echo start`. In OTP one never
   starts source. This changes `wm start`'s argument from `.go` sources (0.2.4) to
   a release directory. The only consumers are `README.md` and the project's own
   tests; both are updated in 0.2.5. No compatibility shim, no argument sniffing.
5. **Cookie handling (resolves a 0.2.4 security backlog item):** the release
   `vm.args` carries `-name` + kernel flags only, **no cookie** — the tarball stays
   secret-free. `wm start` generates a fresh cookie at boot, writes it to a
   `0o600` run-file in the state dir, and boots with a second `-args_file` overlay.
   The cookie never appears on argv and never enters the release/tarball. This
   closes the "cookie-on-argv" item flagged for-fix-before-0.3.0.
6. **`sys.config`:** an empty scaffold `[{<app>, []}].`, actually loaded via
   `-config` to prove the wiring. No Go-side application-env marker in 0.2.5 →
   marker-driven env deferred to the backlog.

## Command surface

```
wm release <path>... [--name N] [--out DIR] [--vsn V] [--version X] [--tar]
wm start   <release-dir>          # BREAKING: was `wm start <go-sources>` in 0.2.4
wm stop | status | call | attach  # unchanged — driven by the State-File
```

- `--name` default: `<app>@127.0.0.1` (validated by the existing `validNodeName`).
- `--out` default: `build/<app>` (release root).
- `--vsn` default: the VERSION file, else `0.0.0` (release build is not fatal on a
  missing VERSION, mirroring 0.2.4 `wm start`).
- `--version` selects the local OTP toolchain (validated by `erlang.ValidateVersion`).
- `--tar` additionally emits `<app>-<vsn>.tar.gz` (no bundled ERTS).

## Release layout

```
build/echo/
  wm.json                                # {app, vsn, node} — single source of truth for wm start
  lib/echo-0.2.5/ebin/{echo.app, *.beam}
  releases/0.2.5/
    echo.rel  echo.script  echo.boot     # .script/.boot from systools:make_script
    sys.config                           # [{echo, []}].
    vm.args                              # -name echo@127.0.0.1  (NO cookie)
  echo-0.2.5.tar.gz                       # only with --tar
```

`wm.json` is a 3-field manifest (`{"app":"echo","vsn":"0.2.5","node":"echo@127.0.0.1"}`)
written by `wm release`, read by `wm start`. It is the single source of truth,
chosen over parsing `-name` out of `vm.args` and globbing `releases/*/` for the
app/vsn — both brittle. One tiny file, trivially testable.

## New package: `internal/pkg/release/`

All logic pure and TDD-covered; the CLI wires it and invokes `systools` via the
existing `erl` seam.

- `RelResource(app, vsn, erts string, apps []AppVsn) string` — the `.rel` term:
  `{release, {App, Vsn}, {erts, E}, [{kernel,K},{stdlib,S},{app,Vsn}]}.`
- `SysConfig(app string) string` — `[{app, []}].`
- `VmArgs(node string) string` — `-name <node>` + kernel flags, no cookie.
- `Manifest{App, Vsn, Node}` with JSON marshal/unmarshal helpers → `wm.json`.

OTP version discovery lives on `erlang.Layout` (it owns OTP-dir knowledge):

- `Layout.ErtsVersion() (string, error)` — glob `$OTP/erts-*`.
- `Layout.AppVersion(name string) (string, error)` — glob `$OTP/lib/<name>-*`
  (used for `kernel`, `stdlib`).

These are pure filesystem globs, unit-tested against a fixture OTP-dir layout.

## `wm release` flow

1. `buildApp(sources, out)` (reused from 0.2.4) → `.beam` + `echo.app` into the
   `lib/echo-<vsn>/ebin/` layout. Requires an application module (same error as
   0.2.4 `wm start` if absent).
2. Discover `erts`, `kernel`, `stdlib` versions from the selected `erlang.Layout`.
3. Write `releases/<vsn>/echo.rel`, `sys.config`, `vm.args`, and `wm.json`.
4. One `erl` invocation via the `captureErl` seam:
   `systools:make_script("echo", [local, {outdir, "releases/<vsn>"}, {path, [...ebin dirs...]}])`
   → `echo.script` + `echo.boot`. The `local` option makes the boot script's paths
   resolve against the release's own `lib/.../ebin` without an install step.
5. `--tar`: a second `erl` invocation, `systools:make_tar("echo", [...])` (no
   `{erts, _}` option) → `echo-<vsn>.tar.gz`.

## `wm start <dir>` refactor

1. Read `<dir>/wm.json` → app, vsn, node.
2. Generate a fresh cookie; write `<state-dir>/<app>.vmargs` (`0o600`) containing
   `-setcookie <cookie>`.
3. Boot detached:
   `erl -detached -boot <dir>/releases/<vsn>/<app> -config <dir>/releases/<vsn>/sys.config -args_file <dir>/releases/<vsn>/vm.args -args_file <state-dir>/<app>.vmargs`
   The boot script starts kernel + stdlib + `<app>` itself — no `-eval` needed.
4. Write the State-File (`{Node, Cookie, CodePath}`) as in 0.2.4. `call`, `stop`,
   `status`, `attach` are unchanged and keep working off the State-File.

## Security

The Erlang cookie is RCE-grade. In 0.2.4 it was passed via `erl -setcookie` on the
detached node's argv, readable by any local user via `ps`/`/proc/<pid>/cmdline`
(flagged in the 0.2.4 backlog as fix-before-0.3.0). In 0.2.5 it lives only in a
`0o600` run-file consumed via `-args_file`: never on argv, never in the release or
tarball. **This closes the cookie-on-argv backlog item.**

## Interop ladder — rungs VI

New rungs prove **release-level interchangeability**: a hand-written-Erlang release
and a Wintermute-generated release of the *same* echo app boot and answer the same
cross-node call identically.

- **VI.1** — hand-written-Erlang release: build `.rel` + boot script from
  hand-written `.erl`, boot detached, `gen_server:call({global, echo}, hello)` → `hello`.
- **VI.2** — Wintermute release: `wm release` the Go echo app, boot the generated
  release, same cross-node call → `hello`.
- **Tarball check** — unpack `echo-<vsn>.tar.gz` on this host and `erl -boot` the
  unpacked release (proves the `--tar` artifact is self-consistent against a
  same-version OTP).

Honest scope: **two release rungs**, not four. Release packaging has no
caller/server cross-product like the gen_server rungs (the caller is always the
control node); the persistent-node rungs V.1–V.4 already cover that axis.

## Testing

- Pure logic (`RelResource`, `SysConfig`, `VmArgs`, `Manifest`, version globs) —
  unit tests, stdlib only, no BEAM.
- `wm release` / `wm start` — unit-tested via the `captureErl`/`runErl`/`attachErl`
  function-var seams (assert assembled command lines without executing `erl`).
- Real `systools` + real boot — the integration ladder (`-tags integration`) on the
  local OTP 29.0.3, rungs VI.1–VI.2 + the tarball check.
- Real CLI e2e — extend/replace `start_integration_test.go` to drive the new
  `wm release → wm start <dir> → status → call → stop` flow on real OTP (the
  regression guard, updated for the two-step interface).

## Out of scope (backlog)

- **ERTS bundling / self-contained tarball** (`systools:make_tar` with `{erts,_}`)
  → 0.2.6 (needs an erlang-free verification environment).
- **Marker-driven `sys.config` env** (a Go `otp.Env{...}` marker) → backlog.
- **relup / appup** (hot code upgrades, `release_handler`, `RELEASES` file) → far out.
- Carried 0.2.4 backlog items unrelated to the cookie fix (control-node name
  uniqueness, `wm ls`, `-heart`, detached-node log file, shared-preamble DRY,
  `stop`/`status` `ValidateVersion`) remain open.

## Verification gate (0.2.5)

`go build -o bin/wm ./cmd/wm`; `go test ./...`; `go test -tags integration
./internal/pkg/ladder/` (all prior rungs + VI); the CLI e2e; `govulncheck ./...`,
`gosec ./...`, `gitleaks detect`. Copilot review gate before any github-bound push.
