# Wintermute

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: Experimental](https://img.shields.io/badge/status-experimental-red)

> *"Wintermute was hive mind, decision maker, effector of change."*
> — William Gibson, Neuromancer

A KI-driven **Go → Erlang/OTP** transpiler. CLI binary: `wm`.

---

## About

Wintermute takes Go source code and transpiles it to Erlang/OTP. Named after the AI in William Gibson's *Neuromancer* that bridges two separate systems into one — just as Wintermute bridges the Go and BEAM worlds.

The transpilation is driven by AI, making it possible to express concurrent, fault-tolerant Erlang/OTP idioms from idiomatic Go code.

---

## Installation

```bash
# Coming soon
go install github.com/bsmr/wintermute/cmd/wm@latest
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

## Status

> ⚠️ **Early Development / Experimental**

This project is in its earliest stages. Nothing works yet. Contributions, ideas, and discussions are welcome.

---

## License

[MIT](LICENSE) © 2026 bsmr
