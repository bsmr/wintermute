# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.2 gen_server service model is implemented and verified; it is ready to merge to `main`.**
It sits on branch `development-0.2.2-work` (executed inline, 9 tasks TDD'd). 0.2.2 is the
**third step of the 0.2.x line**. The completed 0.2.x line is promoted to **0.3.0** after a
further Copilot review + fix session. See the `release-versioning-model` memory.

**0.2.0** (hardening) and **0.2.1** (distributed interop, `b568cdf`) are merged to `main` and
pushed to origin. **0.1.0** remains shipped (`production-0.1.0`).

The echo interop ladder now proves interchangeability at three levels — single-node (rungs
1–4, local `register`/`whereis`), **distributed** (II.1–II.4, two BEAM nodes, `global`), and
**gen_server** (III.1–III.4, the echo as an OTP `-behaviour(gen_server)`). Transpiled
Wintermute is interchangeable with hand-written Erlang in all three.

### 0.2.2 delivered

- **gen_server by convention:** a Go type with `Init`/`HandleCall` methods → `-behaviour(gen_server)`
  with `init/1` + `handle_call/3`. Functional state (receiver in, return out); state field access
  via head pattern-match (`State{Count int}` → `{state, Count}`). New markers `otp.StartServer`/
  `otp.Call` → `gen_server:start_link`/`gen_server:call`.
- **Transpiler capabilities added:** method decls, behaviour detection, callback-signature
  generation, uppercase callback params (Erlang-correctness, reject lowercase — see the
  `erlang-correctness-over-go-idiom` memory), receiver destructuring, field access, `+` binary
  expr, int literals, multi-value return tuples, type-assert strip.
- **Fixtures** (`testdata/genserver/`) + golden tests; **ladder rungs III.1–III.4** via the
  existing single-node `runEcho`.

### Verification gate (all green)

- `go build` + `go test ./...` green.
- **Real integration ladder:** all **12** rungs — single-node 1–4, distributed II.1–II.4, AND
  gen_server III.1–III.4 — PASS on real OTP 29.0.3.
- `govulncheck` / `gitleaks` clean; `gosec` unchanged at the 10 accepted 0.2.0 dual-use findings.

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/   # rungs 1–4 + II.1–II.4 + gen_server III.1–III.4
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar` (GNU/BSD),
plus `libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Next step: merge 0.2.2, then start 0.2.3

1. **Finish 0.2.2** (`superpowers:finishing-a-development-branch`): squash
   `development-0.2.2-work` → `-main` → `main`, push to origin (origin-only 0.2.x step;
   github/upstream + Copilot gate happen at the 0.3.0 promotion of the whole 0.2.x line).
2. **Start 0.2.3 — deployment (bring the service into a running node/cluster):** the OTP
   operation model. Package the gen_server under a **supervisor** in an **Application**;
   `wm` deploys/starts it into a node (rather than boot-and-`init:stop`). This is where the
   dropped "main-based two-node `wm run`" idea is superseded — you deploy a service, not a
   `main()`. Cross-node gen_server (`gen_server:call({global, echo}, …)`) folds in here.
   See the `otp-execution-model-direction` memory.
3. **Evaluate explicitly (own step): native-Erlang interop** — allow hand-written `.erl`
   parts for what Go can't express but OTP needs (records, macros, guards, `.app`/releases).
   See the `native-erlang-interop-open-question` memory. Do after the deployment foundation.

## 0.2.x backlog (deferred, per-task + final reviews)

- **gen_server (0.2.2 deferrals):** `handle_cast`/`handle_info`/`terminate`/`code_change`;
  multiple state fields (the receiver head-pattern binds *all* fields — add `_` for unused
  once multi-field state with unused fields appears); `Init` arguments; multiple gen_server
  instances per module; operators beyond `+`.
- **Embedded/anonymous struct fields** bypass the field-casing guard (`fld.Names` empty)
  and would silent-drop in composite literals — reject `len(fld.Names)==0` explicitly.
  (The one remaining never-silent-wrong edge; out of the echo subset today.)
- `errorf` calls `em.fset.Position` unconditionally — a white-box `emitter{}` on an error
  path would panic (safe today; add a nil-fset guard when white-box error tests appear).
- B6 `tar --version` probe runs *after* the 64 MiB download — reorder before it.
- Cosmetics: single atom-collision test + "duplicate clause" wording; nullary-call roadmap
  message lacks a substring test; multi-line `pat` reindent assumption; size-cap error
  lacks URL context.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** (no `go/types`). It errors on anything
  outside that subset — by design (never silent-wrong), now with `file:line:` positions.
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from `main`.
  Develop on `origin`. Copilot review gate runs before github-bound commits.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored scratch.

## Key artifacts

- 0.2.0 spec: `docs/superpowers/specs/2026-07-10-wintermute-0.2.0-design.md`
- 0.2.0 plan: `docs/superpowers/plans/2026-07-10-wintermute-0.2.0.md`
- 0.1.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.1.0*`
- Verified sources + local build record + pinned SHA-256: `docs/verified-sources.md`
- SDD progress ledger (gitignored, this host): `.superpowers/sdd/progress.md`
- Project rules: `CLAUDE.md`
