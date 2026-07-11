# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-11.

## Where things stand

**0.2.0 hardening is implemented and verified; it is ready to merge to `main`.**
It sits on branch `development-0.2.0-work` (16 tasks, each TDD'd and reviewed).
0.2.0 is the **first step of the 0.2.x line**: a pure hardening release that clears
the 0.1.0 review backlog. Later 0.2.x steps add the thesis-advancing features; the
completed 0.2.x line is promoted to **0.3.0** after a further Copilot review + fix
session. See the `release-versioning-model` memory.

**0.1.0** remains shipped: `main`-based `production-0.1.0`, the single-node Go→Erlang
echo interop ladder — transpiled Wintermute is interchangeable with hand-written
Erlang at the process level (rungs 1–4 pass on real OTP 29.0.3).

### 0.2.0 delivered (all 16 backlog items)

- **Correctness (transpile):** atom-collision detection (A1); lowercase-field reject
  (A2); bare-ident emit **with** uppercase guard (A3); `file:line:` error positions via
  threaded `token.FileSet` (A4); roadmap context in out-of-subset messages (A5);
  `indent`/`otpPkgIdent` consolidation (C4).
- **Security & toolchain (erlang/cli):** `--version` validated `^\d+\.\d+\.\d+$` to block
  path traversal (B1); OTP tarball verified against a **pinned SHA-256** before build (B2);
  download timeout + retry + 200 MiB size cap, deterministic size-cap not retried (B3);
  `Installed()` requires both `erl` and `erlc` (B5); `tar --strip-components` capability
  probe before extraction (B6).
- **CLI-I/O & quality:** robust `--version` parser (B4); `--out` flag + build collision
  guard, `run` overwrites (ephemeral), tempdir-isolated tests (C1); `File` returns the
  module name, no header re-parse (C2); `wm run` prints `booting <mod>` (C3); actionable
  `pkg/otp` transpile-only guard message (C5).

### Verification gate (all green)

- `go build` + `go test ./...` green; integration ladder rungs 1–4 green on real Erlang.
- `govulncheck` / `gitleaks` clean; `gosec` (10) + `semgrep` (1) triaged as accepted
  dual-use / user-choice patterns for a local toolchain CLI (real `--version` traversal
  is closed by B1). Details in the SDD ledger.
- **Real end-to-end `wm erlang install`** (download → SHA-256 verify → tar probe →
  `bytes.NewReader`→tar extract → configure/make/install → `erl` reports OTP 29): exit 0.
- Final whole-branch review (opus, `8787838..07bdcb1`): **READY TO MERGE**, no blockers.

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # download+SHA256-verify+build OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/   # rungs 1–4 on real Erlang
```

Local OTP built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar` (GNU/BSD),
plus `libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Next step: merge 0.2.0, then start 0.2.1

1. **Finish 0.2.0** (`superpowers:finishing-a-development-branch`): Copilot review gate
   (github-bound), squash `development-0.2.0-work` → `-main` → `main`, tag/push per the
   gated-remote workflow.
2. **Start 0.2.1 — distributed interop (ladder step II):** the echo across **two BEAM
   nodes** — `epmd`, node names (`-sname`/`-name`), cookies (`~/.erlang.cookie`),
   `net_kernel`, cross-node registration (`global`); `wm run` orchestrates two nodes.
3. **Later 0.2.x — `gen_server` echo:** a Go type implementing `Init`/`HandleCall` →
   `-behaviour(gen_server)` with callbacks; plus the supervisor question.

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
