# Wintermute 0.2.7 — Native Erlang interop (whole-module escape hatch)

Design spec. Written 2026-07-12.

## Goal

Make Wintermute **Go-first but Erlang-capable**: let a project include
hand-written `.erl` modules alongside transpiled Go, for constructs the Go→Erlang
transpiler cannot express but OTP needs (records, macros, complex guards, binary
pattern matching, list comprehensions). This is an *escape hatch*, analogous to
`asm` in C or TypeScript↔JS interop — Go stays the default, native Erlang is
available when the subset is not enough.

Answers the long-standing open question tracked in the
`native-erlang-interop-open-question` memory.

## Scope (whole-module, narrowest useful cut)

**In:**

- `wm build` and `wm release` accept `.erl` files on the command line alongside
  `.go` source paths.
- A `.erl` input **bypasses the transpiler**, is passed through to the release
  build unchanged, `erlc`-compiled into `ebin/` like any transpiled module, and
  listed in the generated `.app` and `.rel`.
- Go↔native interop uses the **existing OTP mechanisms**: a native gen_server
  registers itself (e.g. `{global, X}`) and transpiled Go calls it via
  `otp.Call` / `otp.CallGlobal` / message passing. No compile-time coupling —
  the registered-name string is arbitrary and already decoupled today.

**Out (YAGNI / deferred):**

- **No new calling marker.** Direct synchronous Go→pure-native-function calls
  (`mod:func(Args)` where the native module is not a process) are *not* supported.
  If needed, wrap the function in a thin native server, or wait for the inline
  escape hatch (below). Deferred: an `otp.Apply(module, func, args...)` marker
  could add this later without breaking anything.
- **No native application module.** Native `.erl` modules are always
  libraries/servers; the `application` and `supervisor` behaviour modules stay
  transpiled Go. This keeps `appMod` detection untouched (no `-behaviour`
  text-scanning of `.erl`). Deferred: allow a native `.erl` to be the app entry
  point by scanning for `-behaviour(application)` — see Backlog.
- **No inline escape hatch (option B).** Embedding raw Erlang for individual
  expressions inside a Go module is a separate, larger milestone. Deferred.

## Mechanism

Single integration point: **`buildApp`** in `internal/pkg/cli/cli.go` — both
`wm build` and `wm release` funnel through it, so both gain native support from
one change (DRY).

In `buildApp`, per input path:

- **`.go`** → unchanged: `transpile.Module(src)`, write `<out>/<module>.erl`.
- **`.erl`** → new branch:
  1. Derive the module name from the **file basename** (`greeting.erl` →
     `greeting`). Erlang's toolchain guarantees `-module(x)` matches `x.erl`;
     `erlc` enforces it, so no content parsing is needed.
  2. Validate the module name with `validAppName` (path-safe; rejects `/`, `\`,
     `..`) — the basename is the only injection surface, since it is later
     spliced into filesystem paths and `erlc`/`systools` `-eval` terms. Full
     atom validity (a weird-but-separator-free basename) is enforced downstream
     by `erlc`, which fails the build if `x.erl` is not a valid module.
  3. Copy the file through to `<out>/<module>.erl`. The **existing
     "already exists (refusing to overwrite)" guard** catches collisions,
     including a native module clashing with a transpiled Go module of the same
     name — for free.
  4. Append the module name to `modules`. It is **not** added to `registered`
     (that key is informational in `.app`; a native module's registered names
     cannot be known without running it, and it self-registers at runtime) and
     never sets `appMod` (native modules are non-application by scope).

Downstream is unchanged: `wm release` `erlc`-compiles every `.erl` in the stage
(transpiled + native) into `ebin/`, and `AppResource`/`.rel` list all modules.

## Security

A `.erl` file is arbitrary Erlang, i.e. arbitrary code at build and run time —
but it is **hand-written by the same user who wrote the Go source**, at the same
trust level as their `.go` input. Compiling it is no more privileged than
compiling their Go. The only new injection surface is the **module name
(basename)**, which flows into filesystem paths and `-eval` terms; `validAppName`
(rejects `/`, `\`, `..`) closes the traversal surface (same guard already used
for the transpiler-derived `appMod`). A malicious basename like `../../x` is
defused twice: `filepath.Base` strips the directory and `validAppName` rejects
separators. Any remaining non-atom basename fails at `erlc`, not at a trust
boundary (the file is the user's own).

No new `gosec` category is expected beyond the accepted `G204` (running the
user's `erlc` on the user's source is the whole point).

## Testing (TDD, red → green)

Three layers, each testing a distinct thing. The key insight (surfaced while
scoping): Go↔native interop **at the BEAM/boot level is already proven** by
existing ladder rung III.2 (a hand-written `echoserver.erl` + transpiled Go
client booted together) — Erlang does not care where a `.beam` came from. The
*genuinely new* code path is `buildApp` routing a `.erl` input through the real
`wm release` pipeline; the ladder's `buildEchoRelease` helper hand-assembles the
release and **bypasses `buildApp`**, so the primary e2e for the new code lives in
the **CLI package**, not the ladder.

**1. Unit (`internal/pkg/cli/cli_test.go`):** `buildApp` with a `.erl` input —
- module name (from basename) listed in `modules`, file copied to
  `<out>/<module>.erl`, no transpile error;
- a `.erl` whose basename collides with a Go module (or an existing file) hits
  the exists-guard;
- an invalid basename is rejected by `validAppName`;
- `appMod` detection is unaffected by native inputs.

**2. CLI integration (`internal/pkg/cli`, `-tags integration`) — the primary
new-path e2e:** drive the real `wm release` with a **mixed input set**
`[go_app, go_sup, native_server.erl, go_client]`, boot the built release against
real OTP, and assert the Go client receives `hello`. This is the only test that
exercises `buildApp`'s `.erl` branch through the actual CLI pipeline. Reuses the
existing `selfcontained`/`start` integration harness in that package.

**3. Ladder rung (`internal/pkg/ladder`, `-tags integration`) — belt-and-
suspenders:** feed the native server into `buildEchoRelease` (which already
accepts `erls []string`, hand-written or transpiled) alongside the Go app/sup/
client, boot, assert `hello`. This re-confirms OTP-behaviour interchangeability
inside a supervised release (it does not touch `buildApp`, so it complements
rather than duplicates layer 2).

**Fixture (`testdata/native/`):** the native echo server is a lightly-enhanced
clone of the existing reference `testdata/genserver/erlang/echoserver.erl` —
its `{state, Count}` tuple becomes a real **`-record(state, {count = 0})`** and a
**guard** is added — so the fixture also demonstrates the *motivation* (records +
guards the Go transpiler cannot emit) in ~8 lines. The app, supervisor, and
client stay transpiled Go (native modules are non-application by scope).

## Files touched

- `internal/pkg/cli/cli.go` — `buildApp` native-`.erl` branch (the only logic
  change).
- `internal/pkg/cli/cli_test.go` — unit tests (layer 1).
- `internal/pkg/cli/*_integration_test.go` — mixed-input `wm release` e2e
  (layer 2, the primary new-path test).
- `testdata/native/` — new fixture: native `.erl` echo server (`-record` +
  guard) + Go app/sup/client.
- `internal/pkg/ladder/ladder_native_integration_test.go` — belt-and-suspenders
  rung (layer 3).
- Usage strings for `wm build` / `wm release` — mention `.erl` inputs.
- `README.md` — short "native Erlang modules" note.
- No transpiler change. No `pkg/otp` change (SDK index unchanged).

## Backlog (deferred here, tracked)

- **Native application module:** allow a hand-written `.erl` to carry
  `-behaviour(application)` and be the app entry point; requires a
  `behaviour`/`behavior` text-scan in the `.erl` branch to set `appMod`.
- **`otp.Apply(module, func, args...)` marker:** direct synchronous Go→native
  pure-function calls without a server wrapper (one new transpiler marker →
  `module:func(args)`).
- **Inline escape hatch (option B):** embed raw Erlang for individual
  expressions inside a Go module (marker syntax + transpiler pass-through).

## Version

**0.2.7** — a feature step on the 0.2.x line (per the `release-versioning-model`
memory: `X.Y.z` = feature step; promotion to `0.3.0` is a separate review+fix
ceremony, not triggered by adding a feature).
