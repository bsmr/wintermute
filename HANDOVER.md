# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-12.

## Where things stand

**0.3.0 — the promotion of the feature-complete 0.2.x line — is RELEASED:
squash-merged to `main` = `54d6855`, tagged `v0.3.0`, pushed to ALL THREE remotes
(`origin`, `upstream`, `github`); `production-0.3.0` created (origin only —
upstream/github carry no `production-*`).** Per the `release-versioning-model`,
`X.(Y+1).0` is a **review + fix** consolidation, not a feature release: 0.3.0
adds NO new capability, no transpiler change, no `pkg/otp` change. It closes the
0.2.x line (single-node → distributed → gen_server → application → persistent
node → release → self-contained target system → native interop).

The 5-task build was subagent-driven (fresh implementer + two-stage review per
task). The final whole-branch review (opus) returned "ready to merge"; the
Copilot gate on the release diff found **no exploitable defects**.

### 0.3.0 delivered (all hardening / cleanup)

- **Control-node cookie off argv (the core fix):** the short-lived control nodes
  for `wm stop`/`status`/`call`/`attach` now load the RCE-grade Erlang
  distribution cookie from a `0o600` `erl -args_file` (new `cookieArgsFile` in
  `internal/pkg/cli/node.go`) instead of `-setcookie` on argv (previously visible
  via `/proc`/`ps` for the sub-second lifetime). This closes the last
  cookie-on-argv residual from 0.2.5. Each command `defer`s the temp-file cleanup
  on every path.
- **DRY + validation:** a shared `controlTarget` preamble (`cli.go`) replaces the
  duplicated `resolveApp → parseVersionFlag → readState → NewLayout` in
  `stop`/`status`/`attach`, and folds in `erlang.ValidateVersion` (previously
  unenforced for `stop`/`status`/`call`/`attach`; `call` gets it inline).
- **security-review sweep** (opus, whole 0.2.x surface — **no Critical/Important
  found**; four Minor hardenings folded by user decision):
  - `validVsn` rejects `..`.
  - `Untar` (`internal/pkg/release/archive.go`) rejects absolute/traversing
    symlink targets and surfaces the `os.Symlink` error (was `_ =`).
  - the generated `bin/stop` (`StopScript`) halts non-zero on `{badrpc, _}`
    (unreachable node) instead of always exiting 0.
  - `validAppName` (`node.go`) rejects shell/Erlang-dangerous characters
    (`" ' \` $ ; ( ) { } [ ] < > |`, whitespace) — still accepts dotted versions
    (`0.3.0`) and lowercase app/module names (it is overloaded to validate both
    `m.App`/app names AND `m.Vsn`).
- **Test hygiene:** the integration tests no longer leak detached `beam.smp`
  nodes. `bin/stop`'s exit status cannot be trusted for cleanup (async
  `init:stop/0` + a stale baked-in rpc target once the test rewrites `vm.args`),
  so `t.Cleanup` now SIGKILL-sweeps any node rooted at the test's unique temp dir
  (`pkill -9 -f <unpack>`). Plus a `strings.CutSuffix` nit in `resolveApp`.

### Verification gate (all green) — 0.3.0, run 2026-07-12 on final `main` (54d6855)

- `go build -o bin/wm ./cmd/wm` clean; `go test ./...` all 5 packages green.
- `go test -tags integration ./internal/pkg/ladder/` — 24 rungs (forced re-run,
  31.9s; `release.go` changed so the cache was invalidated).
- `go test -tags integration ./internal/pkg/cli/` — 0.2.5 e2e + rung VII + native
  e2e, 23.9s; **`pgrep -xc beam.smp` = 0 after** (the leak fix works).
- `govulncheck` clean; `gitleaks` clean.
- `gosec ./...` — **53 findings** (was 52 in 0.2.7; +1 Medium/Low from the new
  `Untar` symlink error path). **5 HIGH, all `G703`** (path-traversal-via-taint on
  wm's own artifacts) — the same accepted dual-use class as 0.2.4–0.2.7. No new
  unaccepted HIGH/CRITICAL category.

### Copilot review gate (pre-github) — clean

No exploitable defects. Two informational notes (no security impact, backlog
only): `cookieArgsFile` uses shared `/tmp` (but `0o600` protects); a SIGKILL
orphans a `0o600` cookie file in `/tmp` (owner-only, no leak).

## Build & test

```bash
go build -o bin/wm ./cmd/wm
go test ./...
./bin/wm erlang install                              # OTP 29.0.3 -> ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/ladder/     # 24 rungs
go test -tags integration ./internal/pkg/cli/        # 0.2.5 e2e + rung VII + native e2e
```

**Integration-test gotcha (now mitigated):** the CLI/ladder integration tests
boot detached BEAM nodes; 0.3.0 added a SIGKILL sweep so a failed `bin/stop` no
longer leaks a node. If a suite still fails oddly (e.g. after a hard interrupt),
clear the env first: `pkill -9 -x beam.smp; pkill -9 -x epmd`, then re-run. See
the `integration-test-leftover-nodes` memory.

## Next step: 0.3.x transpiler-language work (the new line)

0.3.0 is merged, tagged, and on all remotes — nothing pending. The 0.3.x line is
the **transpiler-language / OTP-behaviour completion** (agreed during the coverage
discussion). Suggested cut (start each with `superpowers:brainstorming`):

- **0.3.1 — the language core:** function parameters (drop the "only nullary"
  restriction), `case`/`switch` → Erlang `case`, comparison/boolean operators,
  guards, and tail-recursion (falls out of parameters + recursion; Erlang's LCO is
  free). This is the load-bearing step; the behaviour/callback work below is then
  mostly marker recognition.
- **0.3.2 — full gen_server callbacks:** `handle_cast`/`handle_info`/`terminate`/
  `code_change` (marker recognition, little new language surface).
- **0.3.3 — `gen_statem` / `gen_event`:** new behaviour detection + callback sets.

Key framing (from the coverage analysis): the transpiler should cover only what
maps cleanly to Erlang; loops, list comprehensions, and mutable state stay in the
native-`.erl` escape hatch (0.2.7), NOT the transpiler.

## Backlog (deferred)

- **0.3.0 informational nits (no security impact):** `cookieArgsFile` could use a
  `0o700` state-dir instead of shared `/tmp`; a SIGKILL orphans the `0o600` cookie
  temp file.
- **`bin/stop` async-stop residual:** Fix 3 makes it `halt(1)` on an unreachable
  node, but on the success path `init:stop/0` is async, so exit 0 does not prove
  the node is dead the instant `bin/stop` returns. Inherent to `init:stop/0`;
  operational, not security.
- **Integration-test cleanup block** is duplicated verbatim across
  `native_`/`selfcontained_integration_test.go` (each `unpack` is its own
  `t.TempDir()`); a shared helper would be marginal.
- **relup/appup hot upgrades** — groundwork present (`RELEASES`/`start_erl.data`);
  no `release_handler` upgrade flow, no appup generation.
- **`bin/attach`** for the target system — erts ships `to_erl`/`run_erl`;
  `bin/start` boots via `erl -detached`, so `to_erl` cannot attach.
- **Native-interop follow-ups (from 0.2.7):** native application module
  (`-behaviour(application)` scan), an `otp.Apply(module, func, args...)` marker
  for direct Go→pure-native-function calls, and the inline escape hatch (option B).
- **shared command-preamble DRY:** `controlTarget` covers the control-node
  commands; `start`/`release` still have their own preambles.
- **`absEbin` unescaped** in the `make_script`/`make_tar` `-eval` (self-inflicted
  via `--out`, no trust boundary; deliberately not fixed in 0.3.0).
- **transpiler subset (deferred, now the 0.3.x line):** function parameters,
  `case`/guards/operators, `handle_cast`/`handle_info`/`terminate`/`code_change`,
  multiple state fields, `Init` args, multiple gen_servers per module, supervisor
  strategy/child-spec selection, `gen_statem`/`gen_event`, richer `.app`.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path.
- **Transpiler is go/ast-only for the echo subset** — errors on anything outside
  it, by design. **0.3.0 does not touch the transpiler.**
- **The control-node cookie is loaded via `-args_file`, never on argv** (0.3.0);
  `validAppName` is overloaded to validate app names AND `m.Vsn` — do NOT tighten
  it to an atom-only charset (that breaks dotted versions).
- **Integration tests SIGKILL-sweep leftover nodes** (0.3.0); a clean `bin/stop`
  exit does not prove the target node died (async `init:stop/0`).
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from
  `main`, in order origin→upstream→github. Copilot review gate runs before
  github-bound commits; findings are folded via re-squash before the push.
  Handover/doc-only commits go to origin and reach the gated remotes with the
  next release.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored
  scratch. **Always regenerate `task-N-brief` fresh** (`scripts/task-brief`) — the
  fixed path leaks stale content between versions.
- `testdata/` Go fixtures are not built by `go test ./...`; they are only read as
  source by the transpiler / integration tests.

## Key artifacts

- 0.3.0 spec: `docs/superpowers/specs/2026-07-12-wintermute-0.3.0-promotion-design.md`
- 0.3.0 plan: `docs/superpowers/plans/2026-07-12-wintermute-0.3.0-promotion.md`
- earlier specs/plans: `docs/superpowers/{specs,plans}/2026-07-1{0,1,2}-wintermute-0.2.*`
- SDK index: `docs/SDK-INDEX.md` (unchanged in 0.3.0 — no `pkg/` change)
- Verified sources + local build record: `docs/verified-sources.md`
- Project rules: `CLAUDE.md`
