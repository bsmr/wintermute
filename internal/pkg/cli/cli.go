// Package cli implements the wm command dispatcher.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// commands lists the subcommands wm will support. build and erlang are real;
// the rest are stubs for now.
// ponytail: single stub dispatcher; give each command a real handler when it does something.
var commands = map[string]string{
	"build":  "transpile Go source to Erlang",
	"run":    "transpile and run directly",
	"check":  "type-check and analyse",
	"new":    "scaffold a new project",
	"repl":   "start an interactive REPL (Erlang shell)",
	"erlang": "manage local Erlang/OTP toolchains (install|list)",
}

// Run dispatches a wm subcommand. All I/O is injected for testability.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stdout)
		return nil
	}

	cmd := args[0]
	if _, ok := commands[cmd]; !ok {
		usage(stderr)
		return fmt.Errorf("unknown command: %q", cmd)
	}

	switch cmd {
	case "build":
		return buildCmd(args[1:], stdout)
	case "run":
		return runCmd(ctx, args[1:], stdout)
	case "erlang":
		return erlangCmd(ctx, args[1:], stdout)
	}

	return fmt.Errorf("%s: not implemented yet", cmd)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: wm <command> [args]")
	fmt.Fprintln(w, "\ncommands:")
	for _, name := range []string{"build", "run", "check", "new", "repl", "erlang"} {
		fmt.Fprintf(w, "  %-7s %s\n", name, commands[name])
	}
}

// buildCmd transpiles each Go source path to Erlang, writing <out>/<module>.erl
// (out defaults to bin, overridable via --out) and refusing to overwrite. When
// exactly one input is an OTP application module, it also writes <out>/<app>.app,
// with vsn from --vsn or the VERSION file.
func buildCmd(args []string, stdout io.Writer) error {
	vsn, rest, err := parseVsnFlag(args)
	if err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wm build <path>... [--out DIR] [--vsn X]")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var modules, registered []string
	var appMod string
	for _, path := range rest {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			return err
		}
		dst := outPath(out, r.Module)
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
		}
		if err := os.WriteFile(dst, []byte(r.Erl), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, dst)
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			if appMod != "" {
				return fmt.Errorf("more than one application module (%s and %s)", appMod, r.Module)
			}
			appMod = r.Module
		}
	}
	if appMod != "" {
		if vsn == "" {
			vsn, err = readVersion()
			if err != nil {
				return err
			}
		}
		appFile := filepath.Join(out, appMod+".app")
		body := transpile.AppResource(appMod, vsn, modules, registered)
		if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, appFile)
	}
	return nil
}

// parseVsnFlag pulls an optional --vsn X (or --vsn=X); empty if absent.
func parseVsnFlag(args []string) (vsn string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--vsn":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--vsn requires a value")
			}
			vsn = args[i+1]
			i++
		case strings.HasPrefix(a, "--vsn="):
			vsn = strings.TrimPrefix(a, "--vsn=")
		default:
			rest = append(rest, a)
		}
	}
	return vsn, rest, nil
}

// readVersion reads and trims the project VERSION file in the working directory.
func readVersion() (string, error) {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "", fmt.Errorf("no --vsn given and cannot read VERSION: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// runCmd transpiles the Go source file at args[0] to Erlang, writes it to
// <out>/<module>.erl (out defaults to bin, overridable via --out), compiles
// it with the version's erlc, and boots it with erl, invoking
// <module>:main(). Unlike buildCmd, runCmd has no collision guard: run is an
// ephemeral compile-and-execute step that must be repeatable, so it always
// overwrites its output.
func runCmd(ctx context.Context, args []string, stdout io.Writer) error {
	version, rest, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: wm run <path> [--version X] [--out DIR]")
	}
	srcPath := rest[0]
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	erl, mod, err := transpile.File(string(src))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "booting %s\n", mod)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	dst := outPath(out, mod)
	if err := os.WriteFile(dst, []byte(erl), 0o644); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	if err := runErl(ctx, ".", l.Erlc(), "-o", out, dst); err != nil {
		return err
	}
	eval := mod + ":main(), init:stop()."
	return runErl(ctx, ".", l.Erl(), "-noshell", "-pa", out, "-eval", eval)
}

// parseVersionFlag pulls an optional --version flag (--version X or
// --version=X) out of args, returning the resolved version (DefaultVersion if
// absent), the remaining positional args, and an error on a malformed flag.
func parseVersionFlag(args []string) (string, []string, error) {
	version := erlang.DefaultVersion
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--version requires a value")
			}
			version = args[i+1]
			i++
		case strings.HasPrefix(a, "--version="):
			version = strings.TrimPrefix(a, "--version=")
		default:
			rest = append(rest, a)
		}
	}
	return version, rest, nil
}

// parseOutFlag pulls an optional --out DIR out of args (default "bin").
func parseOutFlag(args []string) (out string, rest []string, err error) {
	out = "bin"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--out":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--out requires a directory")
			}
			out = args[i+1]
			i++
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		default:
			rest = append(rest, a)
		}
	}
	return out, rest, nil
}

// outPath resolves the output .erl path for a module in the given directory.
func outPath(dir, mod string) string {
	return filepath.Join(dir, mod+".erl")
}

// erlangCmd manages local Erlang/OTP toolchains under ~/.local/erlang/.
func erlangCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wm erlang <install|list>")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		root := filepath.Join(home, ".local", "erlang")
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.IsDir() && erlang.NewLayout(home, e.Name()).Installed() {
				fmt.Fprintln(stdout, e.Name())
			}
		}
		return nil
	case "install":
		version, rest, err := parseVersionFlag(args[1:])
		if err != nil {
			return err
		}
		if err := erlang.ValidateVersion(version); err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("usage: wm erlang install [--version X]")
		}
		b := erlang.Builder{Home: home, Out: stdout, Run: execRunner}
		return b.Provision(ctx, version)
	default:
		return fmt.Errorf("unknown erlang subcommand: %q", args[0])
	}
}

// Runner executes name with args in dir. It abstracts command execution so
// tests can assert assembled commands without running anything real.
type Runner = func(ctx context.Context, dir, name string, args ...string) error

// runErl is the package-level indirection used by runCmd to invoke erlc and
// erl; tests may override it.
var runErl Runner = execRunner

// execRunner runs a real command, streaming output.
func execRunner(ctx context.Context, dir, name string, cmdArgs ...string) error {
	c := exec.CommandContext(ctx, name, cmdArgs...)
	c.Dir = dir
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}
