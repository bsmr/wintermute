# Wintermute

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: Experimental](https://img.shields.io/badge/status-experimental-red)
[![Release](https://img.shields.io/github/v/release/bsmr/wintermute)](https://github.com/bsmr/wintermute/releases)

> *"Wintermute was hive mind, decision maker, effector of change."*
> — William Gibson, Neuromancer

A **Go → Erlang/OTP** transpiler. CLI binary: `wm`.

---

## About

Wintermute takes Go source code and transpiles it to Erlang/OTP. Named after the AI in William Gibson's *Neuromancer* that bridges two separate systems into one — just as Wintermute bridges the Go and BEAM worlds.

Wintermute *itself* is developed AI-first; the transpiler it produces is a
deterministic Go-AST → Erlang compiler, not an LLM at runtime. The goal: express
concurrent, fault-tolerant Erlang/OTP idioms from idiomatic Go code.

---

## Installation

```bash
go install go.muehmer.eu/wintermute/cmd/wm@latest
```

Installs the `wm` binary via the `go.muehmer.eu/wintermute` vanity import path
(resolved to the GitHub mirror). Pin a release with `@v0.2.3` instead of
`@latest`. Package docs: <https://pkg.go.dev/go.muehmer.eu/wintermute>.

---

## Usage

```bash
# Transpile Go source to Erlang
wm build main.go

# Transpile and run directly
wm run main.go

# Type-check and analyse
wm check main.go

# Scaffold a new project
wm new myproject

# Start an interactive REPL (Erlang shell)
wm repl

# Build and start a persistent node hosting an OTP application
wm release echo_app.go echo_sup.go echo_server.go --out build/echo
wm start build/echo

wm status echo         # is it up? which apps run?
wm call --app echo echo hello  # cross-node gen_server:call({global, echo}, ...)
wm attach echo         # interactive remote shell (detach leaves it running)
wm stop echo           # clean shutdown

# Build a self-contained target system that runs on a host with NO Erlang
wm release echo_app.go echo_sup.go echo_server.go --out build/echo --self-contained
#   -> build/echo/echo-<vsn>.tar.gz  (bundles ERTS + all apps)
# On the target host (no Erlang needed):
tar xzf echo-<vsn>.tar.gz && ./echo-<vsn>/bin/start   # ./echo-<vsn>/bin/stop to shut down
```

### Native Erlang modules (escape hatch)

Wintermute is Go-first but Erlang-capable. When the Go subset cannot express
what OTP needs (records, macros, complex guards, binary pattern matching, list
comprehensions), hand-write a `.erl` module and pass it to `wm build`/`wm release`
alongside your `.go` sources:

    wm release app.go sup.go server.erl --out dist

The `.erl` file bypasses the transpiler, is compiled natively with `erlc`, and is
packaged into the release. Transpiled Go and native modules interoperate through
the normal OTP mechanisms — a native `gen_server` registered as `{global, Name}`
is reachable from Go via `otp.CallGlobal("Name", ...)`. Native modules are
libraries/servers; the application and supervisor modules stay Go.

---

## Roadmap & releases

Wintermute is built milestone-by-milestone, each a tagged, reviewed release. Full
notes: **[github.com/bsmr/wintermute/releases](https://github.com/bsmr/wintermute/releases)**.

| Line | Milestones |
|---|---|
| **0.1.x** | `0.1.0` Go→Erlang echo interop ladder |
| **0.2.x** — deployment | `0.2.0` hardening · `0.2.1` distributed interop · `0.2.2` gen_server model · `0.2.3` OTP application · `0.2.4` persistent node · `0.2.5` full OTP release · `0.2.6` self-contained target system · `0.2.7` native Erlang interop |
| **0.3.0** — promotion | consolidation of the 0.2.x line: control-node hardening + security-review sweep |
| **0.3.x** — transpiler language | `0.3.1` value model (function parameters, `return`, local `:=`, calls-with-args, recursion) · `0.3.2` control flow (full operator set, `if`/`else` → `case`) · `0.3.3` switch (tagged `switch` → `case`, integer-literal normalization) · `0.3.4` type-switch receive (`switch v := otp.Receive().(type)` → multi-clause `receive`) · `0.3.5` plain-value type switch (`switch v := x.(type)` → `case x of`) |
| **next** | wider type switch (non-struct guards, multi-type cases, whole-alias), `switch` guards, full gen_server callbacks, `gen_statem`/`gen_event` |

### What Wintermute transpiles today

The transpiler covers a deliberately small, cleanly-mapping subset of Go:

- **OTP behaviours:** `gen_server` (`init`/`handle_call`), `supervisor`
  (`one_for_one`), `application` (skeleton).
- **Primitives** (via the `otp` marker package): spawn, send/receive, local &
  global register, `gen_server:start_link`/`call`, `io:format`.
- **Language:** structs → tagged tuples, string/int literals, function
  parameters and `return` values, local `:=` bindings, the full
  arithmetic/comparison/boolean operator set, `if`/`else` → `case`, tagged
  `switch` → `case`, self-recursion, single-clause `receive`, the
  type-switch receive (`switch v := otp.Receive().(type)` → multi-clause
  `receive`), and the plain-value type switch (`switch v := x.(type)` over any
  value → `case x of`).

Anything outside the subset — records, complex guards, binary matching, list
comprehensions, macros — is written as a hand-written `.erl` module (the native
escape hatch above) that interoperates with the transpiled Go through OTP. The
0.3.x line widens the transpiler itself.

---

## Prerequisites

`wm erlang install` builds Erlang/OTP from source under `~/.local/erlang/<ver>/`.
It requires a C compiler (`cc`/`gcc`), `make`, `m4`, `perl`, `tar`, and the
`ncurses` and `openssl` development headers (e.g. `libncurses-dev`,
`libssl-dev` on Debian/Ubuntu). `wm erlang install` checks the build tools up front and fails
fast with the missing ones named.

## Status

> ⚠️ **Early Development / Experimental** — current release: **0.3.5**

Early and experimental, but functional. The deployment line (0.2.x) is
feature-complete for the echo interop subset: Go transpiles to real Erlang/OTP
(application → supervisor → gen_server), builds formal and self-contained OTP
releases, runs as a persistent distributed node, and interoperates with
hand-written native Erlang — all on OTP 29. The 0.3.x line is actively widening
the transpiler itself: function parameters, `return` values, local bindings, the
full operator set, `if`/`else` → `case` control flow (making recursion useful),
tagged `switch`, and the type switch — both over a received message
(`otp.Receive()`) and over any plain value (`case x of`) — have shipped.
Contributions, ideas, and discussions are welcome.

---

## License

[MIT](LICENSE) © 2026 Boris Mühmer

