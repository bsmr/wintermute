// Package cli implements the wm command dispatcher.
package cli

import (
	"context"
	"fmt"
	"io"
)

// commands lists the subcommands wm will support. All are stubs for now.
// ponytail: single stub dispatcher; give each command a real handler when it does something.
var commands = map[string]string{
	"build": "transpile Go source to Erlang",
	"run":   "transpile and run directly",
	"check": "type-check and analyse",
	"new":   "scaffold a new project",
	"repl":  "start an interactive REPL (Erlang shell)",
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

	return fmt.Errorf("%s: not implemented yet", cmd)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: wm <command> [args]")
	fmt.Fprintln(w, "\ncommands:")
	for _, name := range []string{"build", "run", "check", "new", "repl"} {
		fmt.Fprintf(w, "  %-7s %s\n", name, commands[name])
	}
}
