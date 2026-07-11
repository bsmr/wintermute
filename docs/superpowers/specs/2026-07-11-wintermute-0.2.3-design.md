# Wintermute 0.2.3 — OTP Application deployment (design)

Date: 2026-07-11. Line step: 0.2.3 (fourth step of the 0.2.x line).

## Goal

Package the 0.2.2 gen_server under a **supervisor** inside an OTP **application**,
and prove interchangeability of transpiled-Wintermute vs. hand-written Erlang at
this new level (ladder rungs IV.1–IV.4). This is option **A2**: the full triple
`application → supervisor → gen_server`, booted via `application:start/1`, which
requires emitting a minimal `.app` resource file.

Staged plan (context, not part of this spec):
- **0.2.3 = A2** — application + supervisor + minimal `.app` (this spec).
- **0.2.4 = B** — persistent node; `wm` keeps a node alive hosting the application.
- **0.2.5 = C** — full OTP release (`releases/`, `sys.config`); conditional on A/B.

## Non-goals (deferred to roadmap)

- Supervisor strategy/intensity/restart/shutdown/type selection — hardcoded in 0.2.3.
- Multiple children, nested supervisors.
- Application dependencies beyond `kernel`/`stdlib`; `.app` `env`, `start_phases`.
- Persistent node lifecycle and full release layout (0.2.4 / 0.2.5).

## Go conventions

All behaviours are expressed by method-set convention, consistent with the 0.2.2
gen_server model (a Go type whose methods map to the behaviour's callbacks). No
new syntax beyond two markers.

### Application — `package echoapp`

```go
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
```

maps to:

```erlang
-module(echoapp).
-behaviour(application).
-export([start/2, stop/1]).
start(_Type, _Args) -> echosup:start_link().
stop(_State) -> ok.
```

- `App` with a `Start` and a `Stop` method → `-behaviour(application)`.
- `Start()` returns the supervisor pid; `otp.StartSupervisor(echosup.Sup{})` maps
  to `echosup:start_link()` (the argument's package name → the supervisor module).
- `Stop()` is required (explicit, no hidden boilerplate) and maps to
  `stop(_State) -> ok.`

### Supervisor — `package echosup`

```go
type Sup struct{}
func (Sup) Init() []otp.Child {
    return []otp.Child{{ID: "echo", Start: echoserver.Start}}
}
```

maps to:

```erlang
-module(echosup).
-behaviour(supervisor).
-export([start_link/0, init/1]).
start_link() -> supervisor:start_link({local, echosup}, ?MODULE, []).
init(_) -> {ok, {{one_for_one, 1, 5},
                 [{echo, {echoserver, start, []}, permanent, 5000, worker, [echoserver]}]}}.
```

- `Sup` with an `Init() []otp.Child` method → `-behaviour(supervisor)`.
- `start_link/0` is generated: `supervisor:start_link({local, echosup}, ?MODULE, [])`
  (supervisor registered under its own module name).
- Each `otp.Child{ID, Start}` becomes a child spec tuple. `Start` is a
  package-qualified function value (`echoserver.Start`) that maps to the child
  start MFA `{echoserver, start, []}`. New transpiler capability: resolve a
  package-qualified function value to `{module, func, []}`.
- SupFlags fixed at `{one_for_one, 1, 5}`; child defaults fixed at
  `permanent, 5000, worker, [<childmod>]`. Selection is a roadmap item; the
  fixed values are a documented 0.2.3 limitation, not a silent guess.

### gen_server — `package echoserver`

**Unchanged from 0.2.2.** Its `start/0` (→ `gen_server:start_link({local, echo},
?MODULE, [], [])`, which returns `{ok, Pid}`) is already a valid child start MFA,
so the supervisor references it directly. No new gen_server code.

### Client — `package echoclient`

**Unchanged from 0.2.2.** `otp.Call("echo", "hello")` reaches the registered
gen_server exactly as before; the supervisor/application layer is transparent to
the caller.

## `.app` resource emission

The `application` behaviour cannot be started via `application:start/1` without a
loaded `.app` resource. For interchangeability the transpiler must emit it (not a
hand-written fixture), so the transpiled branch is complete.

`wm build` is extended to accept **multiple `.go` files** in one invocation:

1. Transpile each file to its `.erl` (loop over the existing single-file path).
2. Collect the produced module names and any `otp.StartServer(name, …)` registered
   names across the set.
3. If one of the modules carries the `application` behaviour, additionally emit
   `<appname>.app` into the output dir.

Generated `echoapp.app`:

```erlang
{application, echoapp,
 [{description, "echoapp"},
  {vsn, "0.2.3"},
  {modules, [echoapp, echosup, echoserver]},
  {registered, [echo]},
  {applications, [kernel, stdlib]},
  {mod, {echoapp, []}}]}.
```

Field derivation (all from existing information — never a drifting second copy):

| Field | Source |
|---|---|
| `description` | app name (the application module's name); `--desc` optional override |
| `vsn` | `VERSION` file; `--vsn` optional override |
| `modules` | all module names transpiled in this invocation |
| `registered` | names gathered from every `otp.StartServer(name, …)` call |
| `applications` | constant `[kernel, stdlib]` |
| `mod` | `{<appmodule>, []}` — the module carrying the application behaviour |

Emitting the `.app` only when an application-behaviour module is present keeps
plain multi-file builds (no application) behaving exactly as today.

## Ladder rungs IV.1–IV.4

New integration helper `runOtpApp` (analogous to `runEcho`):

1. Compile `echoapp.erl`, `echosup.erl`, `echoserver.erl` with `erlc` into `work`.
2. Place `echoapp.app` on the code path (`-pa work`).
3. Boot:
   `application:start(echoapp), echoclient:main(), application:stop(echoapp), init:stop().`

The four rungs form the same interchangeability matrix as rung III, where the
"server" side is now the whole `app + sup + server` triple:

- **IV.1** — Erlang app+sup+server ↔ Erlang client (baseline).
- **IV.2** — Erlang app+sup+server ↔ Wintermute client.
- **IV.3** — Wintermute app+sup+server ↔ Erlang client.
- **IV.4** — Wintermute app+sup+server ↔ Wintermute client.

Expected output every rung: `hello`.

## Fixtures

`testdata/otpapp/`:

```
go/echoapp/main.go        package echoapp    (new)
go/echosup/main.go        package echosup    (new)
go/echoserver/main.go     package echoserver (reuse 0.2.2 genserver)
go/echoclient/main.go     package echoclient (reuse 0.2.2 genserver)
erlang/echoapp.erl        golden             (new)
erlang/echosup.erl        golden             (new)
erlang/echoserver.erl     golden             (reuse)
erlang/echoclient.erl     golden             (reuse)
erlang/echoapp.app        golden             (new)
```

## Testing (TDD, red → green first)

Unit (`internal/pkg/transpile`, stdlib only, no Erlang):
- application behaviour: `App{Start, Stop}` → `-behaviour(application)` + `start/2` + `stop/1`.
- supervisor behaviour: `Sup{Init}` → `-behaviour(supervisor)` + `start_link/0` + `init/1`.
- child spec: `otp.Child{ID, Start: pkg.Fn}` → `{ID, {pkgmod, fn, []}, permanent, 5000, worker, [pkgmod]}`.
- `otp.StartSupervisor(pkg.T{})` → `pkgmod:start_link()`.
- `.app` generation golden test: fields derived as specified.

Unit (`internal/pkg/cli`):
- `wm build` with multiple `.go` files emits N `.erl` + one `.app` when an
  application module is present; emits only `.erl` (no `.app`) otherwise.

Integration (`internal/pkg/ladder`, build tag `integration`, real OTP 29.0.3):
- Rungs IV.1–IV.4 via `runOtpApp`, each asserting `hello`.

New `pkg/otp` markers (`otp.StartSupervisor`, `otp.Child`) get panic-on-native-run
bodies and doc comments, matching the existing marker style, and are covered by
the package's existing marker tests.

## Verification gate (before merge)

- `go build -o bin/wm ./cmd/wm` + `go test ./...` green.
- All ladder rungs — 1–4, II.1–II.4, III.1–III.4, **IV.1–IV.4** — PASS on real OTP.
- `govulncheck` / `gitleaks` clean; `gosec` unchanged at the accepted dual-use findings.

## Roadmap notes carried forward

Deferred items from the 0.2.x backlog remain open (gen_server `handle_cast`/
`handle_info`/`terminate`/`code_change`, multi-field state, operators beyond `+`,
embedded-struct field guard, `errorf` nil-fset guard, B6 `tar --version` reorder).
New 0.2.3 deferrals: supervisor strategy/restart selection, multiple children,
nested supervisors, richer `.app` (deps, `env`, `start_phases`).
