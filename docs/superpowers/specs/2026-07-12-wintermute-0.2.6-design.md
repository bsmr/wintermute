# Wintermute 0.2.6 — self-contained OTP target system (design)

**Date:** 2026-07-12
**Line:** 0.2.x deployment ladder, ERTS bundling (deferred from 0.2.5)
**Module:** `go.muehmer.eu/wintermute`

## Goal

`wm release --self-contained` produces a **standalone OTP target system**: a
tarball that Ops unpacks on a host with **no Erlang installed** and starts via
`./bin/start`. No `wm`, no system Erlang, no secret baked into the artifact. Like
0.2.5, **no transpiler change** — pure CLI/release tooling that extends the 0.2.5
release builder.

This is the ERTS-bundling follow-up deferred from 0.2.5, where `wm release --tar`
produced a metadata tarball that still required an installed same-version OTP.

## Feasibility spike (2026-07-12) — the design is proven, not theorized

A throwaway spike on real OTP 29.0.3 established every load-bearing assumption:

- `systools:make_tar(Rel, [{erts, code:root_dir()}, …])` bundles `erts-<v>/`,
  `lib/`, `releases/` — confirmed.
- The bundled `erts-<v>/bin/erl` is **self-locating** in modern OTP
  (`ROOTDIR=$(find_rootdir "$0" …)`), so it resolves `$ROOTDIR` from its own path.
  **Runtime relocatability is free — no ROOTDIR rewriting needed.**
- **Critical difference from 0.2.5:** the boot script must be **non-local**
  (`systools:make_script` WITHOUT the `local` option). 0.2.5 used `local`, which
  bakes absolute build-time ebin paths (including the system OTP's kernel/stdlib)
  — that is not relocatable. A non-local boot resolves apps from `$ROOTDIR/lib` at
  boot time, from the bundled runtime.
- The unpacked bundle boots and serves under a **fully scrubbed environment**
  (`env -i PATH=/usr/bin:/bin`, no system Erlang, no `ERL_*`/`ROOTDIR` vars): the
  app node hosted echoapp, registered `{global, echo}`, and a scrubbed control
  node (booted with `start_clean.boot`) resolved it and got `hello`. Result:
  `SELF-CONTAINED REPLY=hello`.
- No `target_system` example module ships on this install
  (`sasl-4.4` has `ebin`/`src` but no `examples/`), so the target-system recipe is
  **hand-rolled in Go**. `release_handler:create_RELEASES` (in sasl) is available
  for the `RELEASES` file. The erts launchers to copy live in `$OTP/bin/`
  (`erl`, `start`, `run_erl`, `to_erl`, `epmd`).

## Decisions (brainstorm, 2026-07-12)

1. **Deliverable: full standalone OTP target system** — canonical layout
   (`bin/`, `erts-<v>/`, `lib/`, `releases/{RELEASES,start_erl.data,<vsn>/…}`),
   runnable via `./bin/start` without `wm` or system Erlang. Prepared for
   `release_handler`/relups later (not implemented in 0.2.6).
2. **Command surface:** a `--self-contained` flag on `wm release` (implies
   `--tar`). Without it, 0.2.5 behaviour is unchanged. `wm start` is **unchanged**
   (still boots the 0.2.5 metadata release against local OTP); the target system
   is operated via `bin/start`.
3. **Build: hand-rolled target-system assembly in Go** (no `target_system`
   module available), all at build time on the build host.
4. **Relocatability:** rely on the modern erts self-locating launchers; the only
   requirement is the non-local boot script. No ROOTDIR rewriting.
5. **Cookie: OTP-default `~/.erlang.cookie`.** `bin/start` sets no `-setcookie`;
   erl auto-creates `~/.erlang.cookie` (`0o400`) on first run. No cookie in the
   artifact, none on argv; same-host app/control nodes share it automatically.
   Cross-host distribution is Ops's job (standard Erlang).
6. **Verification: scrubbed-environment boot** (`env -i`) via the ladder, on the
   dev host — no Erlang-free machine required.

## Command surface

```
wm release <sources>... [--tar] [--self-contained] [--name N] [--out DIR] [--vsn V] [--version X]
```

- `--self-contained` implies `--tar`; produces `<app>-<vsn>.tar.gz` as a full
  target system. Mutually compatible with the existing flags.
- Absent → unchanged 0.2.5 behaviour (metadata release; `--tar` = tarball without
  ERTS).
- `wm start`, `wm stop/status/call/attach` unchanged.

## Target-system layout (unpacked)

```
<app>-<vsn>/                      # tarball root, unpack anywhere
  bin/                           # copied from $OTP/bin (self-locating launchers)
    erl  start  run_erl  to_erl  epmd
    stop                         # generated: control-node rpc init:stop
  erts-<ertsvsn>/                # bundled runtime (self-locating)
  lib/{kernel,stdlib,sasl,<app>}-<v>/ebin/…
  releases/
    RELEASES                     # release_handler:create_RELEASES
    start_erl.data               # "<ertsvsn> <relvsn>"
    <vsn>/
      <app>.rel  start.boot  start_clean.boot  sys.config  vm.args
```

`bin/start` is the erts-shipped starter: it reads `releases/start_erl.data` to
find `<ertsvsn>/<relvsn>` and boots `releases/<vsn>/start.boot` (the release boot,
copied/named `start.boot`). `bin/stop` is a generated control-node script.

> **Spike gap to close first in implementation:** the spike proved a *direct*
> `erts-<v>/bin/erl -boot releases/<vsn>/<app>` scrubbed boot. The `bin/start`
> indirection via `start_erl.data`/`start.boot` is the standard OTP target-system
> recipe but was not exercised in the spike. The plan's first task must confirm
> `./bin/start` boots the release under a scrubbed env on real OTP before the rest
> is built (per the `run-real-toolchain-build-early` memory). If `bin/start`'s
> `run_erl` indirection proves fiddly for the echo subset, fall back to a
> generated `bin/start` wrapper that invokes the bundled erts with the non-local
> boot directly (same shape the spike proved).

## Build pipeline (`wm release --self-contained`)

All steps run at build time; the build host has sasl/systools/release_handler.

1. Build the release tree as in 0.2.5, but generate the boot script **non-local**
   (`systools:make_script(<app>, [{path, [<lib ebins>]}])` — no `local`), and add
   `sasl` to the `.rel` apps list (required for the target system / RELEASES).
2. `systools:make_tar(<app>, [{erts, <OtpRoot>}, {path, [<lib ebins>]}])` →
   an ERTS-bundled tarball (`erts-<v>/`, `lib/`, `releases/<vsn>/`).
3. Augment to a target system (Go: `archive/tar`+`compress/gzip` extract →
   mutate → repack):
   - copy `$OTP/bin/{erl,start,run_erl,to_erl,epmd}` into top-level `bin/`.
   - write `releases/start_erl.data` = `"<ertsvsn> <relvsn>"`.
   - copy `releases/<vsn>/<app>.boot` → `releases/<vsn>/start.boot`.
   - generate `releases/<vsn>/start_clean.boot` (kernel+stdlib+sasl) for control
     nodes, and a `bin/stop` control-node script.
   - write `releases/RELEASES` via a build-time `release_handler:create_RELEASES`
     call.
   - repack `<app>-<vsn>.tar.gz`.

`OtpRoot` = `erlang.Layout.OtpLib()` (`Root/lib/erlang`), already known from 0.2.5.

## New/changed code

- `internal/pkg/release/`: pure helpers for the target-system assembly —
  `StartErlData(ertsVsn, relVsn) string`, a `bin/stop` script generator, and the
  tar-augment logic (extract → add files → repack) as a testable unit over an
  `io.Reader`/`io.Writer` or a working dir. Pure Go, unit-tested.
- `internal/pkg/cli/release.go`: parse `--self-contained`; when set, generate the
  non-local boot + sasl-in-`.rel`, run `make_tar {erts}` via the `captureErl`
  seam, then invoke the assembly. `release_handler:create_RELEASES` runs via the
  same `erl` seam.
- No transpiler change; `wm start` unchanged.

## Interop ladder — rung VII

New rung(s) prove the self-contained target system boots and serves under a
scrubbed environment:

- **VII.1** — a Wintermute-generated target system: unpack into a fresh dir, boot
  `bin/start` (or the bundled erts + non-local boot) under
  `env -i PATH=/usr/bin:/bin` (no system Erlang), a scrubbed control node
  (`start_clean.boot`) resolves `{global, echo}` and gets `hello`.
- **VII.2 (optional)** — the same from a hand-written-Erlang target system, as an
  interchangeability comparison.

Honest scope: 1–2 rungs (self-containedness is a packaging property, not a
transpiler-interchangeability axis).

## Testing

- Pure logic (`StartErlData`, `bin/stop` script text, tar-augment) — unit tests,
  stdlib only, no BEAM.
- `wm release --self-contained` — unit-tested via the `captureErl`/`runErl` seams
  (assert the non-local `make_script`, `make_tar {erts}`, and `create_RELEASES`
  invocations without a real BEAM), plus assertions on the assembled tar layout.
- Real target system — the integration ladder (rung VII) on OTP 29.0.3: the
  scrubbed-env boot + cross-node call. This is the regression guard for
  self-containedness.

## Out of scope (backlog)

- **relup/appup hot upgrades** — 0.2.6 lays the groundwork (`RELEASES`,
  `start_erl.data`) but implements no upgrade flow.
- **`bin/attach`** — the erts ships `to_erl`/`run_erl`; evaluate whether
  `bin/start` runs under `run_erl` (so `to_erl` attaches) or defer.
- **Cross-host cookie distribution** — Ops concern (standard Erlang).
- **Carried 0.2.5 backlog** (unchanged): control-node cookie-on-argv residual in
  `stop`/`status`/`call`/`attach`; `absEbin` unescaped in the make_script/make_tar
  `-eval` (self-inflicted via `--out`); shared command-preamble DRY; `stop`/
  `status` missing `ValidateVersion`; `strings.CutSuffix` nit.

## Verification gate (0.2.6)

`go build -o bin/wm ./cmd/wm`; `go test ./...`; `go test -tags integration
./internal/pkg/ladder/` (all prior rungs + VII); `govulncheck ./...`,
`gosec ./...`, `gitleaks detect`. Copilot review gate on the staged squash before
the github-bound push (per `CLAUDE.md`); push order **origin → upstream → github**.
