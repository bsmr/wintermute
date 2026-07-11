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
	"start":  "start a persistent node hosting an OTP application",
	"stop":   "stop a running node and clear its State-File",
	"status": "ping a running node and list its applications",
	"call":   "call a globally-registered gen_server on a running node",
	"attach": "interactive erl -remsh to the running node",
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
	case "start":
		return startCmd(ctx, args[1:], stdout)
	case "stop":
		return stopCmd(ctx, args[1:], stdout)
	case "status":
		return statusCmd(ctx, args[1:], stdout)
	case "call":
		return callCmd(ctx, args[1:], stdout)
	case "attach":
		return attachCmd(ctx, args[1:], stdout)
	case "erlang":
		return erlangCmd(ctx, args[1:], stdout)
	}

	return fmt.Errorf("%s: not implemented yet", cmd)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: wm <command> [args]")
	fmt.Fprintln(w, "\ncommands:")
	for _, name := range []string{"build", "run", "start", "stop", "status", "call", "attach", "check", "new", "repl", "erlang"} {
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
	appMod, modules, registered, err := buildApp(rest, out)
	// Print partial/all progress first, before checking for errors, so that
	// successfully-transpiled modules are reported even if a later path fails.
	for _, m := range modules {
		fmt.Fprintln(stdout, outPath(out, m))
	}
	if err != nil {
		return err
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

// buildApp transpiles each Go path to <out>/<module>.erl (refusing to
// overwrite) and reports the application module (empty if none), all module
// names, and all registered names. Shared by `wm build` and `wm start`.
func buildApp(paths []string, out string) (appMod string, modules, registered []string, err error) {
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return appMod, modules, registered, err
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			return appMod, modules, registered, err
		}
		dst := outPath(out, r.Module)
		if _, err := os.Stat(dst); err == nil {
			return appMod, modules, registered, fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
		}
		if err := os.WriteFile(dst, []byte(r.Erl), 0o644); err != nil {
			return appMod, modules, registered, err
		}
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			if appMod != "" {
				return appMod, modules, registered, fmt.Errorf("more than one application module (%s and %s)", appMod, r.Module)
			}
			appMod = r.Module
		}
	}
	return appMod, modules, registered, nil
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

// startCmd transpiles + compiles the given Go sources inline, boots a detached,
// named Erlang node running application:start(<app>), and records the node in a
// State-File so stop/status/call/attach can find it with no args.
func startCmd(ctx context.Context, args []string, stdout io.Writer) error {
	name, rest, err := parseStringFlag(args, "--name", "")
	if err != nil {
		return err
	}
	version, rest, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	vsn, rest, err := parseVsnFlag(rest)
	if err != nil {
		return err
	}
	out, rest, err := parseOutFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wm start <path>... [--name N] [--out DIR] [--version X] [--vsn V]")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	appMod, modules, registered, err := buildApp(rest, out)
	if err != nil {
		return err
	}
	if appMod == "" {
		return fmt.Errorf("no application module among %v; wm start needs one", rest)
	}
	// Emit <app>.app onto the code path so application:start(<app>) can find its
	// resource file when the detached node boots. Unlike `wm build`, start is a
	// run action, not a release build, so a missing VERSION is not fatal: fall
	// back to 0.0.0 rather than erroring.
	if vsn == "" {
		if v, verr := readVersion(); verr == nil {
			vsn = v
		} else {
			vsn = "0.0.0"
		}
	}
	appFile := filepath.Join(out, appMod+".app")
	body := transpile.AppResource(appMod, vsn, modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	for _, m := range modules {
		if err := runErl(ctx, ".", l.Erlc(), "-o", out, outPath(out, m)); err != nil {
			return err
		}
	}
	if name == "" {
		name = appMod + "@127.0.0.1"
	}
	cookie, err := newCookie()
	if err != nil {
		return err
	}
	eval := fmt.Sprintf("application:start(%s)", appMod)
	if err := runErl(ctx, ".", l.Erl(), "-detached", "-name", name,
		"-setcookie", cookie, "-pa", out, "-eval", eval); err != nil {
		return err
	}
	if err := writeState(appMod, NodeState{Node: name, Cookie: cookie, CodePath: out}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "started %s (%s)\n", appMod, name)
	return nil
}

// parseStringFlag pulls an optional "--flag V" / "--flag=V" out of args.
func parseStringFlag(args []string, flag, def string) (string, []string, error) {
	val := def
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == flag:
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a value", flag)
			}
			val = args[i+1]
			i++
		case strings.HasPrefix(a, flag+"="):
			val = strings.TrimPrefix(a, flag+"=")
		default:
			rest = append(rest, a)
		}
	}
	return val, rest, nil
}

// resolveApp returns the app name to act on: the first positional arg, or the
// sole running app if none is given.
func resolveApp(args []string) (string, []string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:], nil
	}
	dir, err := stateDir()
	if err != nil {
		return "", nil, err
	}
	entries, _ := os.ReadDir(dir)
	var apps []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			apps = append(apps, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	switch len(apps) {
	case 0:
		return "", nil, fmt.Errorf("no running node; run wm start")
	case 1:
		return apps[0], args, nil
	default:
		return "", nil, fmt.Errorf("multiple running nodes %v; name one explicitly", apps)
	}
}

// ctrlNode returns a unique-ish control node name for a short-lived erl.
func ctrlNode() string { return "wmctrl@127.0.0.1" }

// stopCmd reads the State-File for the target app, tells its node to shut
// down via a short-lived control node doing rpc:call(Node, init, stop, []),
// then removes the State-File.
func stopCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	eval := fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", st.Node)
	if err := runErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval); err != nil {
		return err
	}
	if err := removeState(app); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "stopped %s\n", app)
	return nil
}

// statusCmd pings the target app's node and lists its running applications
// via a short-lived control node, printing the captured report to stdout.
func statusCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	eval := fmt.Sprintf(
		"io:format(\"~p~n\", [net_adm:ping('%s')]), "+
			"io:format(\"~p~n\", [rpc:call('%s', application, which_applications, [])]), "+
			"init:stop().", st.Node, st.Node)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval)
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	fmt.Fprintf(stdout, "%s (%s):\n%s", app, st.Node, out)
	return nil
}

// callCmd resolves the target app's node and issues
// gen_server:call({global, <name>}, <<"<request>">>) against it via a
// short-lived control node, printing the reply to stdout. <request> is sent
// as an Erlang binary.
func callCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := parseStringFlag(args, "--app", "")
	if err != nil {
		return err
	}
	version, rest, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: wm call <name> <request> [--app APP]")
	}
	name, req := rest[0], rest[1]
	if app == "" {
		app, _, err = resolveApp(nil)
		if err != nil {
			return err
		}
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	// The control node is fresh, so it must connect to the target node and
	// converge the global name registry before {global, <name>} resolves. After
	// a detached boot the registration is not instant, so poll net_adm:ping +
	// global:sync until whereis resolves (bounded) before issuing the call —
	// otherwise the cross-node gen_server:call races the registration and fails.
	eval := fmt.Sprintf(
		"Wait = fun Loop(0) -> erlang:error(global_name_timeout); "+
			"Loop(N) -> net_adm:ping('%s'), global:sync(), "+
			"case global:whereis_name(%s) of "+
			"undefined -> timer:sleep(100), Loop(N - 1); _ -> ok end end, "+
			"Wait(30), "+
			"io:format(\"~s~n\", [gen_server:call({global, %s}, <<%q>>)]), init:stop().",
		st.Node, name, name, req)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-setcookie", st.Cookie, "-noshell", "-eval", eval)
	if err != nil {
		return fmt.Errorf("call failed: %w", err)
	}
	fmt.Fprint(stdout, string(out))
	return nil
}

// attachCmd opens an interactive erl -remsh to the target app's node, wired
// to the real terminal, so the user gets a live Erlang shell. Detaching leaves
// the node running.
func attachCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, rest, err := resolveApp(args)
	if err != nil {
		return err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	// A unique-per-invocation control node avoids clashing with a prior attach.
	ctrl := "wmattach@127.0.0.1"
	return attachErl(ctx, ".", l.Erl(), "-remsh", st.Node, "-name", ctrl, "-setcookie", st.Cookie)
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
