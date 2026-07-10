# Wintermute 0.1.0 — Design

- **Status**: approved (brainstorming), pending spec review
- **Date**: 2026-07-10
- **Branch**: `development-0.1.0-work`

## Purpose

Wintermute is an AI-driven **Go → Erlang/OTP** transpiler. You write *valid Go*
that expresses Erlang/OTP the Erlang way (via a dedicated `otp` package), and
Wintermute emits Erlang **source** which is then compiled by the stock `erlc`
and run on a stock Erlang runtime. Targeting Erlang source (not BEAM bytecode)
reuses the whole OTP toolchain and its optimizations, exactly as Gleam does.

Core thesis of 0.1.0: **transpiled Wintermute code is interchangeable with
hand-written Erlang at the process level**, because both compile to ordinary
Erlang processes on the same runtime.

## Foundational decisions (locked)

- **A — Wintermute source is valid Go.** The transpiler reads the Go AST with
  `go/parser` + `go/ast` + `go/types` and emits `.erl`. Because the source *is*
  Go, `gopls`, `go vet`, `go test`, and VSCode/EMACS Go tooling work out of the
  box — no custom LSP.
- **Stdlib only.** The project depends solely on the Go standard library
  (`go/*`, `os/exec`, `net/http`, `testing`, …). No third-party modules.
- **OTP in Go, not Go in Erlang.** Go concurrency primitives (`go`, `chan`) are
  **not** mapped to Erlang. Instead an explicit `otp` package *is* the Erlang way
  written in Go syntax; the transpiler recognizes its calls and emits the
  corresponding Erlang constructs.

## Scope

### In scope (0.1.0)

A single-node **interop ladder** proving interchangeability, plus the local
Erlang provisioning it runs on.

- **Rung 0** — build a local Erlang from source under `~/.local/erlang/<VER>/`,
  sources retained for debugging.
- **Rung 1** — Erlang echo provider ↔ Erlang consumer (hand-written). Proves
  runtime + protocol. No transpiler.
- **Rung 2** — consumer transpiled from Wintermute; provider stays Erlang.
- **Rung 3** — provider transpiled from Wintermute; consumer Erlang.
- **Rung 4** — both sides Wintermute. Full thesis.

All rungs run in **one BEAM node**, processes found by **registered name**.

### Out of scope (later)

- **0.2.0** — distributed topology (II): two nodes, `epmd`, cookies, `-sname`,
  `net_kernel`, `global` registration, `wm run` orchestrating both nodes.
- `gen_server`/supervisor behaviours (0.2.0+).
- General multi-clause / pattern-matching `receive`.
- TCP (`gen_tcp`) echo.
- Native Go execution of the `otp` package (0.1.0 is transpile-only).
- AI extensions (skills/agents).

## Architecture

```text
Go source (valid Go, uses pkg/otp)
        │  go/parser + go/types
        ▼
 internal/pkg/transpile   ──emit──▶  .erl source
        │
        ▼
   erlc (local Erlang)  ──▶  .beam  ──▶  erl (local Erlang)  ──▶  running processes
```

Two subsystems, wired by the `wm` CLI:

1. **Transpiler** — `internal/pkg/transpile`: pure, testable, Go AST → Erlang
   source string. No side effects; takes a parsed/typed package, returns `.erl`.
2. **Runtime provisioning** — `internal/pkg/erlang`: build/install a local
   Erlang from source, locate its `erlc`/`erl`, keep sources.

### Package layout

```text
cmd/wm/                     # entry point (main() -> run(), wiring only)
pkg/otp/                    # public: the OTP-in-Go marker API
internal/pkg/cli/           # command dispatch
internal/pkg/transpile/     # Go AST -> Erlang source
internal/pkg/erlang/        # local Erlang source build + toolchain lookup
bin/                        # build output: generated .erl and .beam (gitignored)
```

## The `otp` package (0.1.0 surface)

`pkg/otp` is a **marker package**: valid Go with stable, importable identifiers
the transpiler matches on (resolved via `go/types` to package path + name).
Bodies are transpile-only stubs (they panic if run natively); execution happens
on Erlang. Minimal 0.1.0 surface:

```go
package otp

type Pid struct{ /* opaque */ }

func Self() Pid                      // -> self()
func Spawn(fn func()) Pid            // -> spawn(fun ... end)
func Register(name string, p Pid)    // -> register(name, Pid)
func Whereis(name string) Pid        // -> whereis(name)
func Send(to Pid, msg any)           // -> To ! Msg
func Receive() any                   // -> receive <clause> end (single clause in 0.1.0)
func Print(s string)                 // -> io:format("~s~n", [S])
```

## Echo protocol (shared by all rungs)

Identical Erlang terms on both sides — this is what makes the sides
interchangeable:

- request: `{echo, From :: pid(), Text :: binary()}`
- reply:   `From ! {ok, Text :: binary()}`

### Go ↔ Erlang value mapping (0.1.0)

| Go | Erlang |
|---|---|
| `struct Echo{From Pid; Text string}` | `{echo, From, Text}` (lowercased type name = leading atom tag; fields in order) |
| `struct Ok{Text string}` | `{ok, Text}` |
| `string` | binary `<<"...">>` |
| `otp.Pid` | `pid()` |

### Construct mapping (0.1.0)

| Go | Erlang |
|---|---|
| Go package | `-module(name).` |
| exported func `F` with N params | `-export([f/N]).` + `f(...) -> ...` |
| tail self-call (`Serve()`) | tail-recursive loop |
| `otp.Spawn(F)` | `spawn(fun ?MODULE:f/0)` |
| `otp.Register("echo", p)` | `register(echo, Pid)` |
| `otp.Whereis("echo")` | `whereis(echo)` |
| `otp.Send(to, msg)` | `To ! Msg` |
| `otp.Receive().(T)` | `receive {tag, ...} -> ... end` (single clause) |
| `otp.Print(s)` | `io:format("~s~n", [S])` |

### Provider / consumer (Wintermute, rung 4)

Provider and consumer are **separate modules** (each independently swappable for
its Erlang counterpart). The struct type `Echo` carries the leading atom tag, so
the loop function is named `Serve` to avoid an identifier collision.

```go
// provider — module echo_server
func Serve() {
    req := otp.Receive().(Echo)
    otp.Send(req.From, Ok{Text: req.Text})
    Serve() // tail-recursive loop
}

// bootstrap: spawn + register the server under the name "echo"
func Start() { otp.Register("echo", otp.Spawn(Serve)) }

// consumer — module echo_client
func Main() {
    otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
    otp.Print(otp.Receive().(Ok).Text)
}
```

Emitted Erlang:

```erlang
%% echo_server
serve() ->
    receive
        {echo, From, Text} -> From ! {ok, Text}, serve()
    end.

start() -> register(echo, spawn(fun ?MODULE:serve/0)).

%% echo_client
main() ->
    whereis(echo) ! {echo, self(), <<"hello">>},
    receive {ok, Text} -> io:format("~s~n", [Text]) end.
```

## Runtime provisioning (`internal/pkg/erlang`)

- **Source**: official Erlang/OTP GitHub releases. Verified 2026-07-10 — latest
  is **OTP 29.0.3**; tarball URL pattern
  `https://github.com/erlang/otp/releases/download/OTP-<ver>/otp_src_<ver>.tar.gz`
  (a `.sigstore` signature asset is available for verification). Tracked in
  [`docs/verified-sources.md`](../../verified-sources.md); the exact URL and
  default version are re-verified there before use.
- **Layout** (per version, enables parallel versions):

  ```text
  ~/.local/erlang/<ver>/
    src/     # extracted otp_src_<ver>, retained for debugging
    bin/     # installed erl, erlc, ... (configure --prefix target)
    lib/ ...
  ```

- **Build steps** (driven from Go via `os/exec`, stdlib):
  `download → extract to src/ → ./configure --prefix=~/.local/erlang/<ver>
  → make → make install`.
- **Toolchain lookup**: resolve `erlc`/`erl` for a chosen version from
  `~/.local/erlang/<ver>/bin`.
- Building OTP from source is a known, well-trodden path (`kerl` does the same);
  we drive `configure`/`make` directly to control the layout and keep sources,
  without adding a non-Go dependency.

## CLI surface (0.1.0)

- `wm erlang install [--version <ver>]` — provision local Erlang (rung 0).
- `wm erlang list` — list installed versions.
- `wm build <path>` — transpile the Go package at `<path>` to `.erl` under `bin/`.
- `wm run <path> [--version <ver>]` — transpile + `erlc` + run on local Erlang.

`check`, `new`, `repl` remain stubs in 0.1.0.

## Error handling

- Transpiler: unsupported Go constructs produce a clear, located error
  (`file:line: unsupported: <construct>`), never a silent or wrong emission. The
  0.1.0 supported subset is exactly what the echo needs; everything else errors.
- Provisioning: each external step (`download`, `configure`, `make`) surfaces its
  command, exit code, and captured stderr on failure. Errors are returned up to
  `run()`, never logged-and-exited.

## Testing strategy (TDD, red → green first)

- **Transpiler** — table-driven unit tests: Go source snippet → expected Erlang
  string. Pure, fast, stdlib `testing` + `go/parser`. Primary dev surface.
- **Provisioning** — unit-test URL construction, path layout, and command
  assembly by injecting a command runner (`main()`→`run()` dependency injection),
  without running a real build. The real end-to-end build is an opt-in
  integration test (build tag / gated by `-short`), since it is slow and hits the
  network.
- **Ladder E2E** — each rung is a test that runs the echo and asserts the echoed
  text. Rungs needing a built Erlang are gated integration tests.

## Roadmap

- **0.2.0** — distributed interop (II); `gen_server` echo (c) as the idiomatic
  OTP variant.
- **Later** — supervisors, general `receive`, TCP, AI extensions.
