# Wintermute

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: Experimental](https://img.shields.io/badge/status-experimental-red)

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

---

## Prerequisites

`wm erlang install` builds Erlang/OTP from source under `~/.local/erlang/<ver>/`.
It requires a C compiler (`cc`/`gcc`), `make`, `m4`, `perl`, `tar`, and the
`ncurses` and `openssl` development headers (e.g. `libncurses-dev`,
`libssl-dev` on Debian/Ubuntu). `wm erlang install` checks the build tools up front and fails
fast with the missing ones named.

## Status

> ⚠️ **Early Development / Experimental**

Early and experimental, but functional: the echo interop subset transpiles to
real Erlang/OTP (application → supervisor → gen_server) and runs on OTP 29.
The Go surface is still a narrow subset. Contributions, ideas, and discussions
are welcome.

---

## License

[MIT](LICENSE) © 2026 Boris Mühmer

