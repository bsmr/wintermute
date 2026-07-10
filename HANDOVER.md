# Handover — Wintermute

Snapshot for resuming work in a fresh session. Updated 2026-07-10.

## Where things stand

**0.1.0 is shipped, reviewed, and proven end-to-end.** `main` = `production-0.1.0`
= the squashed 0.1.0 release. The single-node Go→Erlang echo interop ladder works:
transpiled Wintermute (valid Go) is interchangeable with hand-written Erlang at the
process level. All four ladder rungs pass on a real OTP 29.0.3 build.

- **Delivered:** `pkg/otp` (transpile-only OTP marker API), `internal/pkg/transpile`
  (valid-Go AST → Erlang source for the echo subset), `internal/pkg/erlang` (local
  OTP source build via system `tar`, toolchain lookup, preflight tool check),
  `internal/pkg/cli` (`wm build` / `run` / `erlang install|list`), gated E2E ladder
  (`internal/pkg/ladder`, rungs 1–4).
- **Stubs (intentional):** `wm check` / `new` / `repl`.
- **Verified:** `go test ./...` green; `go test -tags integration ./internal/pkg/ladder/`
  green on real Erlang (needs a provisioned local OTP — see below).

## Build & test

```bash
go build -o bin/wm ./cmd/wm          # binary -> bin/ (never bare go build)
go test ./...                        # fast unit suite (stdlib only, no Erlang)
./bin/wm erlang install              # build OTP 29.0.3 -> ~/.local/erlang/29.0.3 (~5–6 min)
go test -tags integration ./internal/pkg/ladder/   # rungs 1–4 on real Erlang
```

Local OTP already built on this host: `~/.local/erlang/29.0.3` (OTP 29 / erts 17.0.3).
`wm erlang install` prerequisites: `cc`/`gcc`, `make`, `m4`, `perl`, `tar`, plus
`libncurses-dev` + `libssl-dev`. The preflight check names any that are missing.

## Gotchas

- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub repo path. Import
  internal packages accordingly.
- **Transpiler is go/ast-only for the echo subset** (no `go/types` yet). It errors on
  anything outside that subset — by design (never silent-wrong).
- **Erlang variables must be uppercase**; the transpiler currently maps Go field names
  verbatim (works because echo fixtures use `From`/`Text`). See roadmap High #2.
- **Gated remotes** (`upstream`, `github`) receive only tagged releases from `main`.
  Develop on `origin`. Copilot review gate runs before github-bound commits.
- `.superpowers/` (SDD ledger, task briefs/reports) and `bin/` are gitignored scratch.

## How to start 0.2.0 (fresh)

```bash
git checkout main && git pull
# per CLAUDE.md branch model:
git checkout -b development-0.2.0-main
git checkout -b development-0.2.0-work
# bump the pin:
printf '0.2.0\n' > VERSION   # must match the development-<X.Y.Z> branch names
```

Then brainstorm the 0.2.0 scope into a spec, write a plan, execute (subagent-driven).
Design spec + plan for 0.1.0 live in `docs/superpowers/{specs,plans}/` as the pattern.

## 0.2.0 scope (from the 0.1.0 spec roadmap)

1. **Distributed interop (ladder step II)** — the same echo across **two BEAM nodes**:
   `epmd`, node names (`-sname`/`-name`), cookies (`~/.erlang.cookie`), `net_kernel`,
   cross-node registration (`global`). `wm run` orchestrates two nodes. This is the
   main 0.2.0 goal and brings in the "further OTP elements" to exercise.
2. **`gen_server` echo (ladder variant c)** — a Go type implementing a `gen_server`
   interface (`Init`/`HandleCall`) → `-behaviour(gen_server)` module with callbacks;
   plus supervisor question.

## Roadmap hints from the 0.1.0 Copilot release review

Concrete issues to address as 0.2.0 hardens the transpiler and toolchain. Not bugs in
the 0.1.0 echo deliverable (all unreachable by the echo subset), but real for broader input.

**High**
- `transpile.go` — function names are lowercased for atoms without collision detection:
  Go `Foo()` and `foo()` both → Erlang `foo()` (duplicate clause, silently wrong). Detect
  post-lowercase collisions and error.
- `transpile.go` — struct field names / selector fields emitted verbatim as Erlang
  variables. A lowercase-leading Go field (valid Go) yields invalid Erlang. Validate/reject
  or capitalize-and-alias.
- `internal/pkg/cli/cli.go` — `--version` is unvalidated and flows into
  `NewLayout`→`filepath.Join(... version ...)`: a value like `../../etc` is a path-traversal.
  Validate against `^\d+\.\d+\.\d+$` before use. (Local CLI, self-inflicted, but harden it.)
- `internal/pkg/erlang/build.go` — no checksum/signature verification of the downloaded
  tarball before build (already noted in `docs/verified-sources.md`). Verify the `.sigstore`
  signature or a pinned SHA-256.

**Medium**
- `transpile.go` — `emitExpr` has no bare `*ast.Ident` case, so pre-bound variable
  references aren't supported; broadens the usable subset once added.
- `internal/pkg/cli/cli.go` — `moduleName` re-parses the emitted header by string trimming
  instead of returning the module name from `transpile.File`; brittle, no error path.
- `internal/pkg/erlang/build.go` — HTTP download has no timeout/retry or size sanity check.
- `internal/pkg/cli/cli.go` — positional `--version` parsing is brittle (`--version=X`,
  reordering, or a missing value silently falls back to the default instead of erroring).
- `internal/pkg/erlang/toolchain.go` — `Installed()` checks only `erl`, not `erlc`.

**Low**
- CLI `build`/`run` write to a fixed CWD-relative `bin/` with no `--out` and no collision
  guard (same module name overwrites); cli tests also leave an empty `bin/` dir (use
  `os.Chdir(t.TempDir())`).
- Functions-with-parameters and other out-of-subset constructs error opaquely; add roadmap
  context to the error messages (N2).
- `pkg/otp` markers panic if run natively; consider a build-tag/lint guard against
  accidental `go run` before transpilation.
- `build.go` — the `tar --strip-components` shell-out assumes GNU/BSD tar; no capability check.

## Deferred minors from the 0.1.0 final review (also 0.2.0)

- Transpiler errors lack `file:line:` positions (thread a `token.FileSet`).
- `wm run` takes an unused `stdout` writer — wire a "booting <mod>" line or drop it.
- `moduleName` could use `strings.Cut`; `isOtpCall` duplicates an inline check; the receive
  clause-body indent uses a bespoke 8-space string alongside `indent()` — consolidate.
- No unit test for `wm erlang install` argument parsing (extract a pure `--version` parser).

## Key artifacts

- Spec: `docs/superpowers/specs/2026-07-10-wintermute-0.1.0-design.md`
- Plan: `docs/superpowers/plans/2026-07-10-wintermute-0.1.0.md`
- Verified sources + local build record: `docs/verified-sources.md`
- SDD progress ledger (gitignored, this host): `.superpowers/sdd/progress.md`
- Project rules: `CLAUDE.md`
