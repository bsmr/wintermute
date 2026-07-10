# CLAUDE.md — Wintermute

AI-driven **Go → Erlang/OTP** transpiler. CLI binary: `wm`.
Module: `go.muehmer.eu/wintermute`.

## Language & Communication

- **Replies to the user**: always in German — short, precise, technical.
- **Everything else** (code, commits, code comments, file contents): always in English.
- **Style (all languages)**: precise, concise, technical. No filler.

## Go: strict rules

These are non-negotiable for this project.

- **TDD, red → green first**: write the failing test, watch it fail, then
  implement until green. Tests before implementation, always.
- **main() → run() pattern**: `main()` is a thin wrapper; `run()` is wiring only.
  All application logic lives in a package under `internal/pkg/` and is fully
  covered by TDD. `main()` only calls `run()` and handles `os.Exit`. Errors are
  returned, not logged-and-exited. Dependencies (`context.Context`, `args`,
  `io.Reader`/`io.Writer`) are injected as parameters.
- **DRY & KISS + Go best practices** (Google Go Style Guide): apply and review
  them on every change.

```go
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
```

## Structure

```text
cmd/<app>/         # public entry points (wiring only)
pkg/               # public library packages
internal/pkg/      # internal library packages (all business logic, TDD-covered)
internal/cmd/      # internal tools / generators — same main() → run() pattern
bin/               # build output (gitignored)
```

- Internal packages → `internal/pkg/`; public packages → `pkg/`.
- Internal tools (generators) → `internal/cmd/`, same `main()` → `run()` pattern.

## Build & Tooling

- Binaries always to `bin/`: `go build -o bin/wm ./cmd/wm` — never bare `go build`.
- **No temporary files in the project root.**
- **Avoid Makefiles.** Prefer `go install …` / `go get …` for tasks and tools.
- **Security tests / tooling**:
  - Go tools: install via `go install …`.
  - Python tools: own venv under `.python/venv/`.
  - Node.js tools: avoid. If unavoidable, the install method must be clarified first.

## Third opinion (GitHub Copilot)

Before any commit that goes toward `github`, get a third opinion from GitHub
Copilot as a review gate. Non-interactive invocation (costs AI credits per call):

```bash
gh copilot -- -p "Review the staged git diff for correctness, DRY/KISS, and Go best practices." --allow-all-tools
```

Installed via the `gh copilot` built-in (native binary in `~/.local/share/gh/copilot`).

## Git Workflow (project override)

Deviates from the global convention: this project has no shared read-write team
remote. Three remotes, only `origin` is developed on:

| Remote | URL | Role |
|---|---|---|
| `origin` | `git@git.nebula.muehmer.eu:bsmr/wintermute.git` | private fork — all development happens here |
| `upstream` | `git@git.nebula.muehmer.eu:Go/wintermute.git` | **gated** — release target only |
| `github` | `https://github.com/bsmr/wintermute.git` | **gated** — external mirror, release target only |

- **Development**: on `origin`, feature branches `<name>-main` / `<name>-work`
  (`-main` holds the base from `main`, `-work` is the workspace).
- **Gated remotes** (`upstream`, `github`): never receive direct dev pushes.
  Only fast-forward, squashed merges land on `main` first, then `main` is pushed
  to `upstream`/`github` as **tagged** releases. Milestones may get their own
  named branches.
