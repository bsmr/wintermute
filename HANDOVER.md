# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.1 distributed interop is implemented and verified; it is ready to merge to `main`.**
It sits on branch `development-0.2.1-work` (executed inline, 6 tasks TDD'd). 0.2.1 is the
**second step of the 0.2.x line**. The completed 0.2.x line is promoted to **0.3.0** after
a further Copilot review + fix session. See the `release-versioning-model` memory.

**0.2.0** (hardening, 16-item backlog) is merged to `main` (`83ebc7a`) and pushed to origin.
**0.1.0** remains shipped (`production-0.1.0`).

The echo interop ladder now proves interchangeability at two levels: single-node (rungs 1–4,
one BEAM node, local `register`/`whereis`) and **distributed (rungs II.1–II.4, two connected
BEAM nodes, `global` registry)** — transpiled Wintermute is interchangeable with hand-written
Erlang both in-node and cross-node.

### 0.2.1 delivered

- **Two new `otp` markers:** `RegisterGlobal`/`WhereisGlobal` → `global:register_name`/
  `global:whereis_name` (`pkg/otp` + two `emitCall` cases in `internal/pkg/transpile`).
  The only change vs the 0.1.0 echo — Pids are already location-transparent cross-node.
- **Distributed fixtures** (`testdata/echo-dist/`): the 0.1.0 echoes with discovery swapped
  to `global`; golden transpile tests lock the mapping offline.
- **Two-node ladder (step II)** in `internal/pkg/ladder` (gated `//go:build integration`):
  `runEchoDist` boots two same-host `-sname` nodes (cookie `wm_test`, `net_adm:ping` +
  `global:sync`, server-ready-file sync, killed after the client run); rungs II.1–II.4.

### Verification gate (all green)

- `go build` + `go test ./...` green.
- **Real integration ladder:** all 8 rungs — single-node 1–4 AND distributed II.1–II.4 —
  PASS on real OTP 29.0.3 (`go test -tags integration ./internal/pkg/ladder/`).
- `govulncheck` / `gitleaks` clean; `gosec` unchanged at the 10 accepted 0.2.0 dual-use
  findings (no new findings from 0.2.1).

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/   # rungs 1–4 + distributed II.1–II.4
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar` (GNU/BSD),
plus `libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Next step: merge 0.2.1, then start 0.2.2

1. **Finish 0.2.1** (`superpowers:finishing-a-development-branch`): squash
   `development-0.2.1-work` → `-main` → `main`, push to origin (origin-only 0.2.x step;
   github/upstream + Copilot gate happen at the 0.3.0 promotion of the whole 0.2.x line).
2. **Start 0.2.2 — `wm run` orchestrates two nodes:** lift the ladder's `runEchoDist`
   orchestration (`epmd`, `-sname`, cookies, `net_adm:ping` + `global:sync`, deploy both
   modules) into production `wm run`, so a two-node echo runs without the test harness.
3. **Later 0.2.x — `gen_server` echo:** a Go type implementing `Init`/`HandleCall` →
   `-behaviour(gen_server)` with callbacks; plus the supervisor question. Long names
   (`-name`/FQDN), multi-host, and `~/.erlang.cookie` handling also live here.

## 0.2.x backlog (deferred from 0.2.0 per-task + final review)

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
