# CLAUDE.md — Wintermute

AI-driven **Go → Erlang/OTP** transpiler. CLI binary: `wm`.
Module: `go.muehmer.eu/wintermute`.

## Language & Communication

- **Replies to the user**: always in German — short, precise, technical.
- **Everything else** (code, commits, code comments, file contents): always in English.
- **Style (all languages)**: precise, concise, technical. No filler.

## Go: main() → run() Pattern

`main()` is a thin wrapper; `run()` is wiring only. Application logic lives in
`internal/pkg/`, receives all dependencies (`context.Context`, `args`, `io.Reader`/`io.Writer`)
as parameters, and is fully unit-tested.

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

- `main()` only calls `run()` and handles `os.Exit` — never in `run()`.
- Errors are returned, not logged-and-exited.
- Every package MUST have `_test.go` files with meaningful coverage. Test first.

## Structure

```text
cmd/wm/            # entry point (wiring only)
internal/pkg/cli/  # command dispatcher and business logic
bin/               # build output (gitignored)
```

## Build

- `go build -o bin/wm ./cmd/wm` — never bare `go build`.

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
