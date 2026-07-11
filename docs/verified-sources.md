# Verified Sources & Versions

Living ledger for the project rule: **every external link/version MUST be
verified for correctness and matching content before use (no 404s).** Add a row
whenever a link or a tool/runtime version is checked or adopted.

Columns:

- **Item** — what it is (tool, runtime, source tarball, …).
- **Version** — the exact version verified/used.
- **URL** — the checked link (source of truth).
- **Verified** — date the URL was confirmed reachable with matching content.
- **Status** — `OK` (reachable, content matches) / `used` / `superseded`.
- **Notes** — signature asset, prefix, etc.

| Item | Version | URL | Verified | Status | Notes |
|---|---|---|---|---|---|
| Erlang/OTP source tarball | 29.0.3 | https://github.com/erlang/otp/releases/download/OTP-29.0.3/otp_src_29.0.3.tar.gz | 2026-07-10 | built | latest release; `.sigstore` signature asset available; pattern `.../OTP-<ver>/otp_src_<ver>.tar.gz` |

### Local build record

| Version | Date | Host | Build time | Disk | Result |
|---|---|---|---|---|---|
| 29.0.3 | 2026-07-10 | x86_64 linux | ~5m33s (333s) | 883 MB | OK — OTP 29 / erts 17.0.3; `erl`/`erlc` under `~/.local/erlang/29.0.3/bin`; echo interop ladder rungs 1–4 all PASS |

**Extraction:** the tarball is fetched over HTTPS from the pinned official URL
and unpacked with system `tar` (`tar xz --strip-components=1`), which preserves
mtimes, symlinks, and modes — required because the OTP build is timestamp-driven
and ships symlinks. `tar` rejects absolute/`..` members; the source is trusted
and pinned. Cryptographic verification of the `.sigstore` signature before build
is a future enhancement (not stdlib-trivial) — track it when useful.

**SHA-256 pin (Task 12 / B2):** the downloaded tarball is now verified against
a pinned SHA-256 (stdlib `crypto/sha256`, no sigstore/third-party) before
extraction — `Provision` aborts with a checksum-mismatch error instead of
extracting unverified bytes.

| Item | Version | SHA-256 | Verified | Notes |
|---|---|---|---|---|
| Erlang/OTP source tarball | 29.0.3 | `f920c660b16794bcb7270d1cbf680f7747c719650bcd6ac449508a32c2a8972a` | 2026-07-11 | pinned in `internal/pkg/erlang/source.go` (`sourceSHA256`); checked in `fetchSource` before every `Provision` build |

## Installed toolchain (this machine)

Security/dev tools installed via `go install` (in `$(go env GOPATH)/bin`) or the
project venv. Not project module dependencies.

| Tool | Version | Install | Verified |
|---|---|---|---|
| govulncheck | v1.6.0 | `go install golang.org/x/vuln/cmd/govulncheck@latest` | 2026-07-10 |
| gosec | latest | `go install github.com/securego/gosec/v2/cmd/gosec@latest` | 2026-07-10 |
| gitleaks | v8.30.1 | `go install github.com/zricethezav/gitleaks/v8@latest` | 2026-07-10 |
| semgrep | 1.169.0 | venv `~/.python/venv/wintermute/` | 2026-07-10 |
| GitHub Copilot CLI | 1.0.70 | `gh copilot` built-in (native binary) | 2026-07-10 |
