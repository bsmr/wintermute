// Package cli implements the wm command dispatcher.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// atomRE, nodeRE and vsnRE are compiled once and reused by the validators.
var (
	atomRE = regexp.MustCompile(`^[a-z][a-zA-Z0-9_]*$`)
	nodeRE = regexp.MustCompile(`^[a-zA-Z0-9_]+@[A-Za-z0-9_.-]+$`)
	vsnRE  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// validAtom reports whether s is a plain lowercase Erlang atom, safe to splice
// unquoted into an `erl -eval` string (e.g. a gen_server registered name).
func validAtom(s string) bool { return atomRE.MatchString(s) }

// validNodeName reports whether s is a safe Erlang node name (name@host),
// safe to splice unquoted (inside single quotes) into an `erl -eval` string.
func validNodeName(s string) bool { return nodeRE.MatchString(s) }

// validVsn reports whether s is a safe version string. It is stricter than
// validAppName because vsn is spliced raw into the generated /bin/sh launcher
// scripts (StartScript/StopScript) — it must reject shell metacharacters
// ($, backtick, quotes, spaces), not just path separators.
func validVsn(s string) bool { return vsnRE.MatchString(s) }

// commands lists the subcommands wm will support. build and erlang are real;
// the rest are stubs for now.
// ponytail: single stub dispatcher; give each command a real handler when it does something.
var commands = map[string]string{
	"build":   "transpile Go source to Erlang",
	"release": "build the on-disk OTP release tree",
	"run":     "transpile and run directly",
	"start":   "start a persistent node hosting an OTP application",
	"stop":    "stop a running node and clear its State-File",
	"status":  "ping a running node and list its applications",
	"call":    "call a globally-registered gen_server on a running node",
	"attach":  "interactive erl -remsh to the running node",
	"check":   "type-check and analyse",
	"new":     "scaffold a new project",
	"repl":    "start an interactive REPL (Erlang shell)",
	"erlang":  "manage local Erlang/OTP toolchains (install|list)",
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
	case "release":
		return releaseCmd(ctx, args[1:], stdout)
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
	for _, name := range []string{"build", "release", "run", "start", "stop", "status", "call", "attach", "check", "new", "repl", "erlang"} {
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
		return fmt.Errorf("usage: wm build <path>... [--out DIR] [--vsn X]" + nativeErlUsageHint)
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
// names, and all registered names. A hand-written .erl path bypasses the
// transpiler and is copied through unchanged. Shared by `wm build` and
// `wm release`.
func buildApp(paths []string, out string) (appMod string, modules, registered []string, err error) {
	for _, path := range paths {
		if strings.HasSuffix(path, ".erl") {
			// Native escape hatch: a hand-written Erlang module. erlc requires
			// -module(x) to match x.erl, so the basename is the module name.
			// Validate it (the only injection surface — it is spliced into the
			// output path and the .app {modules,...} list) and copy the source
			// through unchanged; wm release erlc's it like any transpiled module.
			mod := strings.TrimSuffix(filepath.Base(path), ".erl")
			if !validAppName(mod) {
				return appMod, modules, registered, fmt.Errorf("invalid native module name %q (from %s)", mod, path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return appMod, modules, registered, err
			}
			// Fail fast on a -module/filename mismatch with a clear message rather
			// than deferring to erlc's downstream error — and wm build never erlc's,
			// so it would otherwise emit an .app inconsistent with the source.
			if name, ok := erlModuleName(data); ok && name != mod {
				return appMod, modules, registered, fmt.Errorf("native module name mismatch: %s declares -module(%s) but the file basename is %q", path, name, mod)
			}
			if err := writeModule(outPath(out, mod), data); err != nil {
				return appMod, modules, registered, err
			}
			modules = append(modules, mod)
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return appMod, modules, registered, err
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			return appMod, modules, registered, err
		}
		if err := writeModule(outPath(out, r.Module), []byte(r.Erl)); err != nil {
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

// nativeErlUsageHint documents, in the build/release usage strings, that inputs
// may be hand-written .erl modules alongside Go sources.
const nativeErlUsageHint = "\n  <path> may be a .go source or a hand-written .erl module"

// writeModule writes a module's Erlang source to dst, refusing to overwrite an
// existing file (a name collision between two inputs). Shared by the transpiled
// and native branches of buildApp.
func writeModule(dst string, data []byte) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
	}
	return os.WriteFile(dst, data, 0o644)
}

// erlModuleRe matches an Erlang -module(Name) attribute at the start of a
// whitespace-trimmed line; Name is the atom text (quoted atoms accepted).
var erlModuleRe = regexp.MustCompile(`^-\s*module\(\s*'?([a-zA-Z0-9_@]+)'?\s*\)`)

// erlModuleName returns the module name declared by a hand-written .erl source,
// skipping comment (%) and blank lines. ok is false when no -module attribute is
// found — buildApp does not second-guess such a file; erlc rejects it downstream.
func erlModuleName(src []byte) (name string, ok bool) {
	for _, line := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%") {
			continue
		}
		if m := erlModuleRe.FindStringSubmatch(t); m != nil {
			return m[1], true
		}
	}
	return "", false
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

// startCmd boots a finished release directory (built by `wm release`): it
// reads the release's wm.json manifest for app/vsn/node, generates a fresh
// cookie kept off argv (written to a 0o600 run-file under the state dir and
// loaded via -args_file), boots a detached Erlang node from the release's
// boot script, and records the node in a State-File so stop/status/call/attach
// can find it with no args.
func startCmd(ctx context.Context, args []string, stdout io.Writer) error {
	version, rest, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: wm start <release-dir> [--version X]")
	}
	dir := rest[0]
	data, err := os.ReadFile(filepath.Join(dir, "wm.json"))
	if err != nil {
		return fmt.Errorf("not a release dir (%s): missing wm.json; run wm release first", dir)
	}
	m, err := release.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("corrupt wm.json in %s: %w", dir, err)
	}
	// The node name is read from wm.json, not a CLI flag, but it later gets
	// spliced unquoted into stop/status/call/attach `erl -eval` strings, so a
	// crafted manifest must never reach the boot invocation.
	if !validNodeName(m.Node) {
		return fmt.Errorf("invalid node name %q in wm.json", m.Node)
	}
	// App and Vsn are likewise spliced unquoted into filesystem paths below
	// (relDir/boot/sysConfig/relVmArgs, and the cookie run-file under the
	// state dir), so a crafted manifest carrying "../" must be rejected
	// before any path is constructed or any file is written.
	if !validAppName(m.App) {
		return fmt.Errorf("invalid app name %q in wm.json", m.App)
	}
	if !validAppName(m.Vsn) {
		return fmt.Errorf("invalid vsn %q in wm.json", m.Vsn)
	}
	relDir := filepath.Join(dir, "releases", m.Vsn)
	boot := filepath.Join(relDir, m.App)
	sysConfig := filepath.Join(relDir, "sys.config")
	relVmArgs := filepath.Join(relDir, "vm.args")

	// Generate a fresh cookie and write it to a 0o600 run-file loaded via
	// -args_file: the cookie must never appear on argv (visible via /proc or
	// `ps`) and never be baked into the release tree itself.
	cookie, err := newCookie()
	if err != nil {
		return err
	}
	sd, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sd, 0o700); err != nil {
		return err
	}
	runVmArgs := filepath.Join(sd, m.App+".vmargs")
	if err := os.WriteFile(runVmArgs, []byte("-setcookie "+cookie+"\n"), 0o600); err != nil {
		return err
	}
	// WriteFile's mode only applies on creation; O_TRUNC over a pre-existing file
	// keeps its (possibly looser) permissions. Chmod unconditionally so the
	// RCE-grade cookie is owner-only even if a stale run-file was left behind.
	if err := os.Chmod(runVmArgs, 0o600); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	if err := runErl(ctx, ".", l.Erl(), "-detached",
		"-boot", boot, "-config", sysConfig,
		"-args_file", relVmArgs, "-args_file", runVmArgs); err != nil {
		return err
	}
	if err := writeState(m.App, NodeState{Node: m.Node, Cookie: cookie, CodePath: dir}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "started %s (%s)\n", m.App, m.Node)
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
		if !validAppName(args[0]) {
			return "", nil, fmt.Errorf("invalid app name %q", args[0])
		}
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
	if !validAtom(name) {
		return fmt.Errorf("invalid gen_server name %q (must be a lowercase atom)", name)
	}
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
