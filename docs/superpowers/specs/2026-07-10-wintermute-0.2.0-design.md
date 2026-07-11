# Wintermute 0.2.0 — Design

- **Status**: approved (brainstorming), pending spec review
- **Date**: 2026-07-10
- **Branch**: `development-0.2.0-work`

## Purpose

0.2.0 is a **hardening release**. It ships no new user-facing capability; it
clears the entire review backlog accumulated during 0.1.0 (Copilot release
review + whole-branch final review) so the transpiler never emits silently-wrong
Erlang, the toolchain verifies what it downloads and rejects hostile input, and
the accumulated code-quality nits are consolidated.

0.2.0 is the **first** step of the 0.2.x line. The thesis-advancing work
(distributed interop / ladder step II, `gen_server` echo, supervisors) follows in
later 0.2.x steps, each its own minor release. The whole 0.2.x line is promoted
to **0.3.0** only once it is complete and passes a further Copilot review + fix
session — the same gate that closed 0.1.0. 0.2.0 buys the solid base those later
steps build on.

The transpiler stays what it is: a deterministic `go/ast` → Erlang source
compiler, no LLM at runtime.

## Foundational decisions (unchanged from 0.1.0)

- **Wintermute source is valid Go.** `go/parser` + `go/ast` read the source; the
  transpiler emits `.erl`, compiled by stock `erlc`.
- **Stdlib only.** No third-party modules. This decision drives B2 below (pinned
  SHA-256 via `crypto/sha256`, not a sigstore verifier that would pull a foreign
  dependency).
- **Never silent-wrong.** The transpiler errors on anything outside its subset
  rather than emitting plausible-but-wrong Erlang. This principle drives A1 and
  A2 (reject, don't guess).

## Locked design decisions (this release)

- **A2 field-casing → reject.** A lowercase-leading Go struct field (valid Go,
  e.g. `text`) would map to an invalid Erlang variable (variables must be
  uppercase). The transpiler **rejects** it with a clear error rather than
  auto-capitalizing behind an alias table. Consistent with never-silent-wrong;
  keeps "what you write is what you get".
- **B2 tarball verification → pinned SHA-256.** Each supported OTP version
  carries a pinned SHA-256 of its source tarball, verified with `crypto/sha256`
  before the build. Stdlib-only; no sigstore/third-party verifier.

## Scope

### In scope (0.2.0)

Sixteen backlog items, each its own red → green TDD cycle. Grouped by area.

#### Group A — Correctness (`internal/pkg/transpile`)

- **A1 — Atom-collision detection (High).** Function names are lowercased to
  Erlang atoms without collision detection: Go `Foo()` and `foo()` both become
  Erlang `foo()` (duplicate clause, silently wrong). Detect post-lowercase
  collisions across the emitted function set and **error**.
- **A2 — Field-casing rejection (High).** Struct field names / selector fields
  are emitted verbatim as Erlang variables. A lowercase-leading field yields
  invalid Erlang. **Reject** with a clear error (see locked decision above).
- **A3 — `emitExpr` bare `*ast.Ident` (Medium).** `emitExpr` has no bare
  `*ast.Ident` case, so pre-bound variable references are unsupported. Add the
  case; broadens the usable subset.
- **A4 — Error positions (minor).** Transpiler errors lack `file:line:`
  positions. Thread a `token.FileSet` through so every transpiler error carries
  a position.
- **A5 — Out-of-subset messages (Low).** Functions-with-parameters and other
  out-of-subset constructs error opaquely. Add roadmap context to the messages.

#### Group B — Security & Toolchain (`internal/pkg/erlang`, `internal/pkg/cli`)

- **B1 — `--version` path-traversal (High).** `--version` is unvalidated and
  flows into `NewLayout` → `filepath.Join(... version ...)`; a value like
  `../../etc` is a path traversal. Validate against `^\d+\.\d+\.\d+$` before use.
- **B2 — Tarball verification (High).** The downloaded OTP tarball is built
  without integrity verification. Verify a per-version pinned SHA-256 with
  `crypto/sha256` before the build (see locked decision).
- **B3 — HTTP download robustness (Medium).** The download has no timeout,
  retry, or size sanity check. Add a bounded timeout, a small retry, and a size
  ceiling.
- **B4 — `--version` parsing (Medium).** Positional `--version` parsing is
  brittle: `--version=X`, reordering, or a missing value silently falls back to
  the default instead of erroring. Extract a pure, unit-tested parser that errors
  on malformed input.
- **B5 — `Installed()` checks `erlc` (Medium).** `Installed()` checks only
  `erl`; a toolchain with `erl` but no `erlc` reads as installed. Check both.
- **B6 — `tar` capability check (Low).** The `tar --strip-components` shell-out
  assumes GNU/BSD tar with no capability check. Probe the flag before relying on
  it and fail clearly if unsupported.

#### Group C — CLI-I/O & Code-quality

- **C1 — `build`/`run` output (Low).** Output goes to a fixed CWD-relative
  `bin/` with no `--out` and no collision guard (same module name overwrites).
  Add `--out`, guard collisions, and switch cli tests to
  `os.Chdir(t.TempDir())` so they stop leaving an empty `bin/`.
- **C2 — `moduleName` return (Medium).** `moduleName` re-parses the emitted
  header by string-trimming instead of returning the module name from
  `transpile.File`. Return it from the transpile step; use `strings.Cut`.
- **C3 — `wm run` stdout (minor).** `runCmd` takes an unused `stdout` writer.
  Wire a "booting `<mod>`" line through it.
- **C4 — `isOtpCall` + indent (minor).** `isOtpCall` duplicates an inline check;
  the receive-clause body uses a bespoke 8-space string alongside `indent()`.
  Consolidate both.
- **C5 — `pkg/otp` panic-guard (Low).** `pkg/otp` markers panic if run natively.
  Add a build-tag guard so accidental `go run` before transpilation fails with a
  clear message rather than a raw panic.

#### Cross-cutting

- **VERSION → `0.2.0`** (bumped, committed with the first task).
- **Security tooling** re-run after the B items land: `govulncheck ./...`,
  `gosec ./...`, `gitleaks detect`, `~/.python/venv/wintermute/bin/semgrep
  --config auto`. B1/B2 are squarely their domain.
- **B2 hash source.** Verify the SHA-256 for OTP 29.0.3 against the source
  recorded in `docs/verified-sources.md`, pin it, and document the pin there.

### Out of scope for 0.2.0 (later in the 0.2.x line)

- Distributed interop / ladder step II (two BEAM nodes, epmd, cookies,
  `net_kernel`, `global`) — a later 0.2.x step.
- `gen_server` echo variant and the supervisor question — a later 0.2.x step.
- Stubs `wm check` / `wm new` / `wm repl` remain stubs for now.

0.3.0 is not a scope bucket but the **promotion target**: the completed 0.2.x
line, after a further Copilot review + fix session.

## Testing

Strict TDD, red → green per item. Security and correctness items lead with a
**negative** test that fails before the fix:

- A1: `Foo()` + `foo()` in one file → transpile error (not duplicate clause).
- A2: struct with a lowercase-leading field → transpile error.
- A4: an out-of-subset construct → error string contains `file:line:`.
- B1: `--version=../../etc` (and other traversal shapes) → rejected before any
  path join.
- B2: a tarball whose bytes don't match the pinned SHA-256 → build aborts.
- B4: `--version=29.0.3`, reordered args, and a missing value each parse to the
  expected result or a clear error — no silent default.
- B5: a toolchain dir with `erl` but no `erlc` → `Installed()` reports false.

Existing suites stay green (`go test ./...`). The integration ladder
(`-tags integration ./internal/pkg/ladder/`) must still pass on the provisioned
OTP 29.0.3 — this release must not regress the proven E2E thesis.

## Delivery

- Branch model per `CLAUDE.md`: work on `development-0.2.0-work`, squash into
  `development-0.2.0-main`, then `main`.
- Copilot review gate before any github-bound commit.
- Real build/ladder run before merge (per the "run real toolchain build early"
  lesson from 0.1.0 — green unit tests + clean reviews are not sufficient).

## Key artifacts

- Plan: `docs/superpowers/plans/2026-07-10-wintermute-0.2.0.md` (next step).
- 0.1.0 spec/plan: `docs/superpowers/{specs,plans}/2026-07-10-wintermute-0.1.0*`.
- Verified sources + local build record: `docs/verified-sources.md`.
- Project rules: `CLAUDE.md`.
