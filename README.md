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
# Coming soon
go install go.muehmer.eu/wintermute/cmd/wm@latest
```

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

This project is in its earliest stages. Nothing works yet. Contributions, ideas, and discussions are welcome.

---

## License

[MIT](LICENSE) © 2026 Boris Mühmer

