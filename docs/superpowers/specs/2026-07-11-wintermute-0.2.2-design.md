# Wintermute 0.2.2 — Design

- **Status**: approved (brainstorming), pending spec review
- **Date**: 2026-07-11
- **Branch**: `development-0.2.2-work`

## Purpose

0.2.2 is the **third step of the 0.2.x line**: the `gen_server` service model. It
expresses the echo as a real OTP behaviour — a Go type with `Init`/`HandleCall`
transpiles to `-behaviour(gen_server)`. This is the first step away from the
`main()` script model toward the service model (deployment/cluster → 0.2.3).

The transpiler stays a deterministic `go/ast` → Erlang source compiler, no LLM at
runtime.

## Why this, and why now

The `main()`/`Main()` run model (transpile → boot → `main()` → `init:stop()`) is a
script/test model — good for proving interchangeability (0.1.0 single-node,
0.2.1 distributed), but not how OTP is operated. Real OTP is long-lived behaviour
processes (`gen_server`, …) in a supervision tree, packaged as an Application,
deployed into a running/starting node. You deploy a service, not a `main()`.
0.2.2 introduces the service (`gen_server`); deployment builds on it in 0.2.3.

Scope this step to **standalone single-node `gen_server`** — the one big new
capability is behaviour/callback transpilation (methods, params, state). Supervisor,
Application, deployment, and cross-node are later steps.

## Guiding principle (locked, from user)

**Wintermute adapts to Erlang; no hidden automatisms.** Erlang has hard rules —
variables must be uppercase-leading, atoms lowercase. When a form is required
because Erlang works that way, Wintermute goes the Erlang-correct way rather than
hiding the mapping behind a rename. Concretely, extending A2/A3: identifiers that
become Erlang **variables** (struct fields, callback params, bound vars) are
written **uppercase-leading** in the Go source and lowercase is **rejected** (not
auto-capitalized). Identifiers that become **atoms** (func/module/registered
names) are lowercased. A receiver name that only destructures away in a head
pattern is exempt (never emitted as a variable).

## Locked decisions (this release)

- **gen_server recognized by convention:** a Go type with `Init` + `HandleCall`
  methods → `-behaviour(gen_server)`.
- **Functional state:** state flows in via the receiver, out via the return.
- **State field access → head pattern-match** (like 0.1.0's receive clause), not
  `element/2`: `State{Count int}` → `{state, Count}`, destructured in the callback
  head, fields as bound variables.
- **Callback params are uppercase** in the Go source (`Req`), per the guiding
  principle. Named returns are omitted (never emitted).
- **Markers** `otp.StartServer(name, T{})` → `gen_server:start_link({local, name},
  ?MODULE, [], [])` and `otp.Call(name, req)` → `gen_server:call(name, Req)`.
- **Client stays a `main()` test-driver**, kept direct (no intermediate variable),
  so the type-assert `.(string)` is the only new client-side construct.

## Scope

### In scope (0.2.2)

#### Fixtures (the target shape)

Server (`testdata/genserver/go/echoserver/main.go`):

```go
package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type State struct{ Count int }

func (State) Init() State              { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) {
	return Req, State{Count: s.Count + 1}
}

func Start() { otp.StartServer("echo", State{}) }
```

Client (`testdata/genserver/go/echoclient/main.go`):

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
```

Hand-written Erlang counterparts (`testdata/genserver/erlang/`):

```erlang
-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

init(_) -> {ok, {state, 0}}.
handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.

start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).
```

```erlang
-module(echoclient).
-export([main/0]).

main() -> io:format("~s~n", [gen_server:call(echo, <<"hello">>)]).
```

#### Transpiler capabilities (the core — each a TDD cycle)

1. **Method declarations** — process `fn.Recv != nil` funcs (today skipped).
2. **Behaviour detection** — a type with `Init` + `HandleCall` methods emits
   `-behaviour(gen_server)` and exports `init/1`, `handle_call/3` (plus `start/0`
   from the package-level `Start`).
3. **gen_server callback signatures** — the transpiler knows the OTP arity: Go
   `Init()` → `init(_) -> {ok, State}`; Go `HandleCall(Req)` on receiver `State`
   → `handle_call(Req, _From, {state, Count}) -> {reply, Reply, NewState}`. It
   supplies the ignored `_From` and the receiver-derived state pattern.
4. **Function parameters** — `Req` → the Erlang variable `Req` (uppercase
   required per the guiding principle; lowercase rejected). Today params are an
   error (the 0.2.x-roadmap message).
5. **Receiver state destructuring** — `(s State)` → head pattern `{state, Count}`;
   only fields used in the body are bound, unused fields become `_` (the
   transpiler knows the used fields from the body).
6. **Field access** — `s.Count` → the bound variable `Count`.
7. **Binary expression** — `s.Count + 1` → `Count + 1` (new `*ast.BinaryExpr`
   case; `+` is enough for the counter — others rejected until needed).
8. **Multi-value return** — `return Req, State{...}` in a `HandleCall` →
   `{reply, Req, {state, Count + 1}}`; `Init`'s `return State{Count: 0}` →
   `{ok, {state, 0}}`.
9. **Type assertion strip** — `otp.Call(...).(string)` → the inner call (Erlang is
   dynamically typed; the assertion is Go-only).
10. **Markers** — `otp.StartServer`/`otp.Call` → `gen_server:start_link`/`gen_server:call`.

#### Ladder (step III, single-node)

Reuse the **existing** `runEcho` helper (`internal/pkg/ladder/ladder_integration_test.go`)
— one node boots `echoserver:start(), echoclient:main(), init:stop()`. New
gated rungs (`//go:build integration`), each asserting `hello`:

- **III.1** erl ↔ erl (hand-written gen_server + caller)
- **III.2** Wintermute caller, Erlang gen_server
- **III.3** Wintermute gen_server, Erlang caller
- **III.4** both Wintermute

### Out of scope (→ 0.2.3+)

- Supervisor, Application/Release, deployment / "bringing a service into a
  running node or cluster".
- `handle_cast` / `handle_info` / `terminate` / `code_change`; multiple state
  fields beyond the counter; init arguments.
- Cross-node gen_server (`gen_server:call({global, echo}, …)`).
- **Native Erlang interop** — allowing hand-written `.erl` parts in a Wintermute
  project for what Go can't express but OTP needs (records, macros, guards,
  `.app`/releases). A strategic escape-hatch to be **evaluated explicitly** as its
  own step after the deployment foundation.

## Testing

Strict TDD, red → green.

- **Unit (`internal/pkg/transpile`):** one test per capability (behaviour header,
  callback signatures, param → uppercase var, receiver destructuring, field
  access, `Count + 1`, multi-return tuples, type-assert strip, markers); golden
  tests for both fixtures.
- **Unit (`pkg/otp`):** the new markers `StartServer`/`Call` panic natively with
  the actionable transpile-only message.
- **Integration (`internal/pkg/ladder`, gated):** rungs III.1–III.4 on real OTP
  29.0.3. Existing single-node rungs 1–4 and distributed II.1–II.4 stay green.

## Delivery

- Branch model per `CLAUDE.md`: work on `development-0.2.2-work`, squash into
  `development-0.2.2-main`, then `main` (origin). Origin-only 0.2.x step; the
  github/upstream tagged release + Copilot gate happen at the 0.3.0 promotion.
- Run the real step-III ladder before merge (green units are not sufficient).

## Key artifacts

- Plan: `docs/superpowers/plans/2026-07-11-wintermute-0.2.2.md` (next step).
- 0.2.1 spec/plan (global markers, distributed ladder): `docs/superpowers/{specs,plans}/2026-07-11-wintermute-0.2.1*`.
- 0.1.0 echo fixtures + `runEcho`: `testdata/echo/`, `internal/pkg/ladder/`.
- Project rules: `CLAUDE.md`.
