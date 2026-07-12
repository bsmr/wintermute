# Wintermute 0.3.0 — Promotion of the 0.2.x line

Design spec. Written 2026-07-12.

## Goal & nature

**0.3.0 is a promotion, not a feature release.** Per the `release-versioning-model`,
`X.(Y+1).0` consolidates the feature-complete previous line through a **review +
fix** session, then tags `v0.3.0`. The 0.2.x line (single-node → distributed →
gen_server → application → persistent node → release → self-contained target
system → native interop) is feature-complete; 0.3.0 hardens and cleans it before
the 0.3.x transpiler-language work begins.

**Explicitly OUT of 0.3.0** (deferred to 0.3.1+): all transpiler language
features (function parameters, `case`/`switch`, comparison/boolean operators,
guards, tail-recursion), full gen_server callbacks (`handle_cast`/`handle_info`/
`terminate`/`code_change`), and `gen_statem`/`gen_event`. 0.3.0 adds **no new
capability**; every change is a fix, a refactor, or a hardening of existing code.

## Bounding rule for the review sweep

0.3.0 includes a genuine `security-review` pass over the 0.2.x attack surface —
but to keep the promotion bounded: **only Critical/Important findings are folded
into 0.3.0. Minor findings and anything feature-shaped go to the 0.3.x backlog.**
The review scope is the existing surface: the CLI commands, the release/archive
code, cookie handling, path/atom validation, and the `systools -eval`
interpolations.

## Work units

### 1. Control-node preamble refactor + cookie-via-args_file

(Helper names below — `cookieArgsFile`, a preamble resolver — are indicative; the
plan fixes exact signatures.)

The heart of 0.3.0 — one refactor that fixes three backlog items at once, because
they live in the same code region (`stop`/`status`/`call`/`attach` in
`internal/pkg/cli/cli.go`).

- **cookie-on-argv (security):** the four short-lived control nodes currently pass
  `-setcookie <cookie>` on argv (visible via `/proc`/`ps` for the sub-second
  lifetime), while the long-lived node (`startCmd`) already loads the cookie from
  a `0o600` `-args_file` (0.2.5). A new helper writes the cookie to a temporary
  `0o600` args_file (mirroring `startCmd`'s `runVmArgs`: `WriteFile` then an
  unconditional `Chmod 0o600`), passes `-args_file <tmp>` instead of
  `-setcookie <cookie>`, and removes the file afterwards (`defer`). Applied to all
  four sites, including `attach` (which uses the interactive `attachErl`/`-remsh`
  path).
- **DRY preamble:** `stop`/`status`/`attach` repeat the identical preamble
  (`resolveApp` → `parseVersionFlag` → `readState` → `os.UserHomeDir` +
  `erlang.NewLayout`); `call` is a near-variant (`--app` flag + `validAtom` +
  `resolveApp(nil)` fallback). Extract the shared preamble into a helper returning
  `(app, st, layout, rest)`.
- **stop/status missing ValidateVersion:** `start`/`run`/`erlang` validate the
  version; `stop`/`status`/`call`/`attach` do not. Fold `erlang.ValidateVersion`
  into the shared preamble so all control-node commands validate once, in one
  place.

The `controlErl` cookie helper and the preamble helper are separate small units:
a `cookieArgsFile(cookie) (path string, cleanup func(), err error)` and a
preamble resolver. `call` reuses the cookie helper but keeps its own arg parsing
(it is not a plain `resolveApp` command).

### 2. Integration-test cleanup (beam.smp leak)

`t.Cleanup` in `internal/pkg/cli/native_integration_test.go` and the pre-existing
`selfcontained_integration_test.go` discards `bin/stop`'s error
(`_ = stop.Run()`), so repeated local runs leak detached `beam.smp` nodes that
pollute epmd and eventually make a run fail for environmental reasons (this bit
the 0.2.7 cycle — see the `integration-test-leftover-nodes` memory). Fix both
tests' cleanup to surface/act on the stop failure (assert `stop.Run()` succeeded,
or add a SIGKILL fallback on the started process).

### 3. Micro-nits

- **`strings.CutSuffix`:** apply where the `HasSuffix`+`TrimSuffix` pattern is
  already being touched (pre-existing linter hint). Pure style; do not chase
  untouched call sites.
- **`absEbin` unescaped in the `make_script`/`make_tar` `-eval`:** **dropped**
  (not fixed). It is self-inflicted via `--out` with no trust boundary (the user
  supplies their own output dir); fixing it is YAGNI. Recorded as a backlog note,
  not a change.

### 4. security-review sweep

Run the `security-review` skill over the 0.2.x surface (scope above). Fold
Critical/Important findings into 0.3.0; route Minor/feature-shaped findings to the
0.3.x backlog. This runs after units 1–3 so it reviews the hardened code.

### 5. Verification gate + Copilot gate + release

- Full gate on final `main`: `go build -o bin/wm ./cmd/wm`, `go test ./...`, both
  integration suites (`-tags integration`) on real OTP 29.0.3, `govulncheck`,
  `gitleaks`, `gosec` (confirm no new unaccepted HIGH/CRITICAL beyond the accepted
  `G204`/`G304`/`G306`/`G703` classes).
- Copilot review gate on the release diff before the github push; fold any real
  findings.
- `VERSION` → `0.3.0`. Squash-merge to `main`, tag `v0.3.0` (annotated), create
  `production-0.3.0`, push origin→upstream→github (main + tag on all three;
  `production-0.3.0` to origin).

## Testing

TDD where behaviour changes:
- **`controlErl` cookie fix:** a test asserting the control-node invocation carries
  **no `-setcookie` on argv** and that the cookie lives in a `0o600` args_file
  (assert on the assembled command args + the temp file's mode/content). This is
  the security-critical proof.
- **Preamble refactor is behaviour-preserving:** the existing `stop`/`status`/
  `call` unit tests and the integration e2e must stay green — they prove the
  extracted helper did not change command behaviour.
- **ValidateVersion in stop/status:** a test that a bad `--version` is rejected by
  `stop`/`status` (previously unvalidated).
- **Integration cleanup:** the fixed `t.Cleanup` leaves no leaked node (best-effort
  assertion; the primary value is the leak no longer accumulating).

## Files touched (anticipated)

- `internal/pkg/cli/cli.go` — `controlErl`/`cookieArgsFile` helper, shared preamble
  resolver, the four control-node commands rewired, `ValidateVersion` folded in,
  `CutSuffix` nit.
- `internal/pkg/cli/cli_test.go` — cookie-off-argv test, ValidateVersion tests,
  preamble-equivalence coverage.
- `internal/pkg/cli/native_integration_test.go`,
  `internal/pkg/cli/selfcontained_integration_test.go` — cleanup fix.
- `VERSION` → `0.3.0`.
- Plus whatever the security-review sweep surfaces (Critical/Important only).
- **No transpiler change, no `pkg/otp` change** (SDK index unchanged).

## Backlog (deferred here, tracked)

- `absEbin` unescaped in the `-eval` (self-inflicted, no trust boundary).
- All 0.3.x transpiler-language work (see the coverage discussion / HANDOVER).
- Any Minor findings the security-review sweep surfaces.
