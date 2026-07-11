# Wintermute 0.2.4 — Persistent Node (detached-first)

Design spec. Fifth step of the 0.2.x ladder. Status: approved, ready for plan.

- **Module path:** `go.muehmer.eu/wintermute` (not the GitHub path).
- **Version:** 0.2.4.
- **Predecessor:** 0.2.3 (OTP application deployment — boot-and-`init:stop`).

## Goal

Move from boot-and-`init:stop` to a **persistent node**: `wm` starts a real,
named Erlang node that keeps the transpiled OTP application alive in the
background, and talks to it afterwards over Distributed Erlang. This proves the
next rung of the echo interop ladder — a running node serving `gen_server`
calls **across nodes** — and lays the deployment foundation that 0.2.5 (full OTP
release) builds on. See the `otp-execution-model-direction` memory.

## Decisions (from brainstorm)

1. **Hosting model:** a standalone, named node — NOT a REPL-hosted node. The
   node runs on its own; an interactive shell is an *attach* tool, not the
   node's lifeline. This mirrors real OTP releases (`bin/app start` +
   `bin/app remote_console`).
2. **Primary mode: detached-first.** `wm start` daemonises the node; `wm stop` /
   `status` / `call` / `attach` drive it over Distributed Erlang.
3. **Native daemonisation:** `erl -detached` — no custom OS fork/setsid,
   cross-platform. The node's *name* (via `epmd`), not a PID, is the handle.
4. **Node identity: State-File.** `wm start` records node name + cookie; the
   other subcommands read it, so they work with no args.
5. **Cross-node call via `wm call`.** A CLI command exercises
   `gen_server:call({global, <name>}, Req)` from a short-lived control node.
6. **Global registration** is required for the cross-node call — new `otp`
   markers, additive; local `StartServer`/`Call` unchanged.
7. **Inline transpile:** `wm start` takes `.go` sources and transpiles +
   compiles them inline (consistent with `wm run`), not prebuilt `.app`/`.beam`.
8. **Separate fixtures** for the persistent/cross-node ladder — the 0.2.3
   fixtures (IV.1–IV.4, `{local, echo}`) stay untouched.

## CLI surface

Five new subcommands, all routed through the existing `Runner`/`runErl`
indirection in `internal/pkg/cli/cli.go` so the assembled `erl` commands are
assertable in unit tests without executing anything real.

| Command | Effect | Erlang underneath |
|---|---|---|
| `wm start <app.go>… [--name N] [--out DIR]` | transpile+compile inline, boot **detached**, write State-File | `erl -detached -name <node> -setcookie <c> -pa <dir> -eval "application:start(<app>)"` |
| `wm status` | read State-File, check reachability + running apps | control node: `net_adm:ping/1` + `rpc:call(Node, application, which_applications, [])` |
| `wm call <name> <req>` | cross-node gen_server call, reply to stdout | `gen_server:call({global, <name>}, <req>)` from a short-lived control node |
| `wm attach` | interactive remote shell (detach leaves node running) | `erl -remsh <node> -name <ctrl> -setcookie <c>` (real TTY, exec) |
| `wm stop` | clean shutdown, remove State-File | `rpc:call(Node, init, stop, [])` |

`wm start` mirrors `wm run`: it takes `.go` sources, not prebuilt artifacts.

## Node identity (State-File)

- Path: `$XDG_STATE_HOME/wintermute/<app>.json`, default
  `~/.local/state/wintermute/<app>.json`.
- Content: `{ "node": "<app>@127.0.0.1", "cookie": "<generated>", "codepath": "<workdir>" }`.
- Node name default `<app>@127.0.0.1`, overridable via `--name`. Cookie is
  generated at `start` and stored. `stop`/`status`/`call`/`attach` with no args
  read the file; `stop` removes it.
- `wm ls` (list all running) is a later addition, not 0.2.4.

## Marker API (`pkg/otp`)

Additive, mirroring the existing `Register`/`RegisterGlobal` pair:

- `StartServerGlobal(name, init)` → `gen_server:start_link({global, name}, ?MODULE, [], [])`
- `CallGlobal(name, req)` → `gen_server:call({global, name}, req)`

Local `StartServer`/`Call` are unchanged. The transpiler grows the two new
markers; the `.app` / supervisor / application emission from 0.2.3 is reused
as-is (the globally-registered server is still a `permanent` worker child).

## Fixtures & ladder V.1–V.4

New fixture set under `testdata/persistent/` (Go + hand-written Erlang), with a
**globally** registered echo server. Four rungs prove cross-node
interchangeability:

- **V.1** Erlang app ↔ Erlang caller
- **V.2** Erlang app ↔ Wintermute caller
- **V.3** Wintermute app ↔ Erlang caller
- **V.4** Wintermute app ↔ Wintermute caller

Each rung: `start` a detached node hosting the app → cross-node
`gen_server:call({global, echo}, Msg)` → assert reply → `stop`. The 0.2.3
rungs IV.1–IV.4 remain unchanged and green.

## Data flow

```
wm start echo.go …
  └─ transpile.Module → .erl + .app  →  erlc → .beam in workdir
     └─ erl -detached -name echo@127.0.0.1 -setcookie C -pa workdir
              -eval "application:start(echoapp)"      (node stays alive)
     └─ write ~/.local/state/wintermute/echoapp.json {node,cookie,codepath}

wm call echo "hi"
  └─ read State-File
     └─ erl -name ctrl@127.0.0.1 -setcookie C -noshell
              -eval 'io:format("~s~n",[gen_server:call({global,echo},<<"hi">>)]), init:stop().'
     └─ stdout: hi

wm stop
  └─ read State-File
     └─ erl -name ctrl@… -setcookie C -noshell
              -eval 'rpc:call(Node, init, stop, []), init:stop().'
     └─ remove State-File
```

## Error handling

- **Detached diagnosis:** a detached node has no shell, so a failed
  `application:start` is silent. The node writes its logs to a file in the state
  dir via the kernel logger (`-kernel logger '[{handler,default,logger_std_h,
  #{config=>#{file=>"<statedir>/<app>.log"}}}]'` or equivalent); `wm status`
  surfaces that file's tail when the node is unreachable. Minimal — no logging
  framework, exact flag pinned during implementation against OTP 29.
- `wm stop`/`status`/`call`/`attach` with a missing or stale State-File return
  an actionable error (`no running node for <app>; run wm start`).
- Unreachable node (`net_adm:ping` = `pang`): `wm status` reports down and
  points at the log; `wm call`/`stop` fail with the node name and cookie hint.
- Node-name collisions: default `<app>@127.0.0.1`; `--name` for a second
  instance. Tests use unique names (see risks).

## Testing

- **Unit (fast, stdlib-only):** the five subcommands assert assembled `erl` /
  `erlc` command lines through the `runErl` `Runner` seam — no real Erlang.
  State-File read/write/remove tested directly. Marker transpilation
  (`StartServerGlobal`/`CallGlobal`) tested in `transpile_test.go`.
- **Integration (ladder V.1–V.4, `-tags integration`):** real OTP 29, detached
  nodes, cross-node calls. Each test uses a **unique node name** to avoid `epmd`
  collisions under parallel runs, and tears the node down in a `t.Cleanup`.

## Deferrals (0.2.4 backlog)

- `wm ls` (all running nodes); full `--cookie`/`--name` surface; `-heart`
  restart; multiple apps per state dir; log rotation.
- Everything carried over from the 0.2.3 backlog (supervisor strategy knobs,
  `handle_cast`/`handle_info`, richer `.app`, etc.).

## Non-goals

- Full OTP release (`releases/`, `sys.config`) — that is 0.2.5, conditional on
  this rung holding.
- Native-Erlang interop (hand-written `.erl` for records/macros/guards) — its
  own explicit step after the deployment foundation. See the
  `native-erlang-interop-open-question` memory.
