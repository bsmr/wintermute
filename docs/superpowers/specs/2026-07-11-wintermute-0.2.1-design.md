# Wintermute 0.2.1 — Design

- **Status**: approved (brainstorming), pending spec review
- **Date**: 2026-07-11
- **Branch**: `development-0.2.1-work`

## Purpose

0.2.1 is the **second step of the 0.2.x line**: distributed interop (ladder step
II). It proves the same echo across **two connected BEAM nodes** — transpiled
Wintermute is interchangeable with hand-written Erlang cross-node, not only
inside a single node as 0.1.0 proved.

The transpiler stays what it is: a deterministic `go/ast` → Erlang source
compiler, no LLM at runtime.

## Core insight

The delta from the 0.1.0 echo is minimal. `otp.Self()` and `otp.Send(From, …)`
are already location-transparent cross-node (an Erlang Pid carries its node, and
sending to a remote Pid is transparent). **Only service discovery changes**: from
the node-local `register`/`whereis` registry to the cluster-wide `global`
registry. The echo code therefore knows **no node names** — only the test
orchestration does.

Two BEAM nodes on one host are genuinely distributed: each `erl -sname` is a
separate OS process / BEAM node with its own heap, scheduler, and Pid range; they
connect over localhost TCP via `epmd` + a shared cookie + `net_kernel`, and
`global` synchronizes over that link exactly as it would across machines. The
only thing same-host does not exercise is physical distribution, which is
irrelevant to the protocol/interchangeability proof.

## Foundational decisions (unchanged from 0.1.0/0.2.0)

- **Wintermute source is valid Go**, transpiled to `.erl`, compiled by stock `erlc`.
- **Stdlib only.** No third-party modules.
- **Never silent-wrong.** The transpiler errors on anything outside its subset.
- **main() → run()**; all logic in `internal/pkg/`; strict TDD.

## Locked decisions (this release)

- **Cross-node addressing → `global` registry.** New markers `otp.RegisterGlobal`
  / `otp.WhereisGlobal` transpile to `global:register_name` / `global:whereis_name`.
  The code stays node-name-free (location-transparent), idiomatic Erlang for named
  cross-node services.
- **Ladder orchestration → test-driven** in `internal/pkg/ladder`, not a
  production orchestration helper. `wm run` orchestrating two nodes is 0.2.2; the
  ladder proving the thesis is this step, consistent with how 0.1.0 used the
  ladder (`runEcho`) rather than `wm run`.
- **Same-host two nodes via `-sname`** (short names). Long names (`-name`/FQDN)
  and multi-host are 0.2.2+.

## Scope

### In scope (0.2.1)

#### 1. New `otp` markers + transpiler (`pkg/otp`, `internal/pkg/transpile`)

Two transpile-only markers and two new `emitCall` cases, modeled on the existing
`Register`/`Whereis` cases (atom name via `unquoteAtom`, Pid arg):

| Go marker | Erlang |
|---|---|
| `otp.RegisterGlobal(name, pid)` | `global:register_name(name, Pid)` |
| `otp.WhereisGlobal(name)` | `global:whereis_name(name)` |

`pkg/otp` gains the two markers (transpile-only, panic natively via the existing
`transpileOnly` helper). The rest of the transpiler is untouched.

#### 2. Distributed fixtures (`testdata/echo-dist/`)

Four files — the 0.1.0 echoes with discovery swapped to `global`:

- `go/echoserver/main.go` — `Start()` uses `otp.RegisterGlobal("echo", otp.Spawn(Serve))`; `Serve()` unchanged.
- `go/echoclient/main.go` — `Main()` uses `otp.WhereisGlobal("echo")`; the rest unchanged.
- `erlang/echoserver.erl` — `global:register_name(echo, spawn(fun serve/0))`.
- `erlang/echoclient.erl` — `global:whereis_name(echo)`.

#### 3. Ladder step II orchestration (`internal/pkg/ladder`)

A new `runEchoDist` helper that starts two nodes on this host:

- **Node A (server):** `erl -sname wm_echo_server -setcookie wm_test -noshell -pa <work> -eval "echoserver:start()"` — a background process that stays alive (its spawned `serve/0` loops) until killed.
- **Node B (client):** `erl -sname wm_echo_client -setcookie wm_test -noshell -pa <work> -eval "net_adm:ping('wm_echo_server@<host>'), global:sync(), echoclient:main(), init:stop()"` — blocking; stdout captured.
- After the client exits, the helper kills node A and asserts the client output is `hello`.

Details:
- **`<host>`** is the short hostname (`os.Hostname()`, first label) — matches `-sname`'s `name@shorthost` form.
- **Isolation:** a dedicated cookie `wm_test` via `-setcookie`; it does **not**
  touch `~/.erlang.cookie`. `epmd` is auto-started by `erl -sname`.
- **`net_adm:ping` + `global:sync()`** live only in the client `-eval` (orchestration), keeping the echo code node-name-free. `global:sync()` forces the global name tables to converge after the connection is established, so `global:whereis_name(echo)` reliably resolves the remote server Pid.
- **Server lifecycle:** node A has no `init:stop()`; it is killed by the helper (`cmd.Process.Kill`) after the client run. `runEchoDist` guarantees the kill even on client failure (deferred).

#### 4. Ladder rungs (step II)

Four rungs, mirroring 0.1.0 (each rung swaps one side), all under
`//go:build integration`:

- **II.1** erl ↔ erl — distributed baseline: node setup + `global` protocol, no transpiler.
- **II.2** Wintermute client, Erlang server.
- **II.3** Wintermute server, Erlang client.
- **II.4** both Wintermute — the full distributed thesis.

### Out of scope (→ 0.2.2+)

- `wm run` orchestrating two nodes (CLI UX).
- `gen_server` / supervisor.
- Long names (`-name`/FQDN), multi-host, `~/.erlang.cookie` handling.
- Node failure/reconnect, `global` conflict resolution.

## Testing

Strict TDD, red → green.

- **Unit (`internal/pkg/transpile`):** `RegisterGlobal` → `global:register_name(echo, …)`; `WhereisGlobal` → `global:whereis_name(echo)`. Assert the emitted Erlang; the name is a bare atom (via `unquoteAtom`), the Pid arg is emitted as an expression.
- **Unit (`pkg/otp`):** the two new markers panic natively with the actionable transpile-only message (naming the symbol).
- **Integration (`internal/pkg/ladder`, gated):** the four step-II rungs, each asserting `hello`. Existing single-node rungs 1–4 stay green. Needs provisioned local OTP 29.0.3.

The step-II integration rungs are the thesis proof; the unit tests lock the
transpiler mapping offline.

## Delivery

- Branch model per `CLAUDE.md`: work on `development-0.2.1-work`, squash into
  `development-0.2.1-main`, then `main` (origin). 0.2.1 is an origin-only 0.2.x
  step; the github/upstream tagged release + Copilot gate happen at the 0.3.0
  promotion of the whole 0.2.x line.
- Run the real integration ladder (step II on real OTP 29.0.3) before merge —
  green unit tests are not sufficient (the "run real toolchain build early" lesson).

## Key artifacts

- Plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.1.md` (next step).
- 0.2.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.2.0*`.
- 0.1.0 echo fixtures + ladder: `testdata/echo/`, `internal/pkg/ladder/`.
- Project rules: `CLAUDE.md`.
