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

// buildCmd transpiles the Go source file at args[0] to Erlang and writes it
// to bin/<module>.erl, printing the output path to stdout.
func buildCmd(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: wm build <path>")
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	erl, err := transpile.File(string(src))
	if err != nil {
		return err
	}
	mod := moduleName(erl)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	outPath := filepath.Join("bin", mod+".erl")
	if err := os.WriteFile(outPath, []byte(erl), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, outPath)
	return nil
}

// runCmd transpiles the Go source file at args[0] to Erlang, writes it to
// bin/<module>.erl, compiles it with the version's erlc, and boots it with
// erl, invoking <module>:main().
func runCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: wm run <path> [--version X]")
	}
	version := erlang.DefaultVersion
	if len(args) == 3 && args[1] == "--version" {
		version = args[2]
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	erl, err := transpile.File(string(src))
	if err != nil {
		return err
	}
	mod := moduleName(erl)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("bin", mod+".erl"), []byte(erl), 0o644); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	if err := runErl(ctx, ".", l.Erlc(), "-o", "bin", filepath.Join("bin", mod+".erl")); err != nil {
		return err
	}
	eval := mod + ":main(), init:stop()."
	return runErl(ctx, ".", l.Erl(), "-noshell", "-pa", "bin", "-eval", eval)
}

// moduleName extracts the name from "-module(name)." on the first line.
func moduleName(erl string) string {
	first := erl
	if i := strings.IndexByte(erl, '\n'); i >= 0 {
		first = erl[:i]
	}
	first = strings.TrimPrefix(first, "-module(")
	return strings.TrimSuffix(first, ").")
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
		version := erlang.DefaultVersion
		if len(args) == 3 && args[1] == "--version" {
			version = args[2]
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
