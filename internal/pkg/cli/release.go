package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// releaseCmd builds a formal OTP release tree from Go sources. It does not boot;
// `wm start <dir>` boots the result.
func releaseCmd(ctx context.Context, args []string, stdout io.Writer) error {
	tar := false
	selfContained := false
	var filtered []string
	for _, a := range args {
		switch a {
		case "--tar":
			tar = true
		case "--self-contained":
			selfContained = true
			tar = true // --self-contained implies --tar
		default:
			filtered = append(filtered, a)
		}
	}
	args = filtered

	name, rest, err := parseStringFlag(args, "--name", "")
	if err != nil {
		return err
	}
	if name != "" && !validNodeName(name) {
		return fmt.Errorf("invalid node name %q (must match name@host)", name)
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
		return fmt.Errorf("usage: wm release <path>... [--name N] [--out DIR] [--version X] [--vsn V] [--tar] [--self-contained]")
	}
	if vsn == "" {
		if v, verr := readVersion(); verr == nil {
			vsn = v
		} else {
			vsn = "0.0.0"
		}
	}

	// Transpile to a staging dir first so we learn the app module name before we
	// can name the versioned lib/<app>-<vsn>/ebin directory.
	stage := filepath.Join(out, ".stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	appMod, modules, registered, err := buildApp(rest, stage)
	if err != nil {
		return err
	}
	if appMod == "" {
		return fmt.Errorf("no application module among %v; wm release needs one", rest)
	}
	// vsn and appMod are spliced into filesystem paths and the systools -eval
	// term below. vsn can come from an untrusted --vsn or a poisoned VERSION file,
	// so guard it as a safe path segment (rejects "", "/", "\\", "..") before any
	// path is built — symmetric with startCmd's validAppName(m.Vsn) on the read
	// side. appMod is transpiler-derived and atom-safe by construction, but the
	// same guard makes the make_script("%s") interpolation self-evidently safe.
	if !validVsn(vsn) {
		return fmt.Errorf("invalid vsn %q (must be [A-Za-z0-9._-]+)", vsn)
	}
	if !validAppName(appMod) {
		return fmt.Errorf("invalid app module %q", appMod)
	}

	ebin := filepath.Join(out, "lib", appMod+"-"+vsn, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	for _, m := range modules {
		if err := runErl(ctx, ".", l.Erlc(), "-o", ebin, outPath(stage, m)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(ebin, appMod+".app"),
		[]byte(transpile.AppResource(appMod, vsn, modules, registered)), 0o644); err != nil {
		return err
	}

	relDir := filepath.Join(out, "releases", vsn)
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		return err
	}
	erts, err := l.ErtsVersion()
	if err != nil {
		return err
	}
	kernel, err := l.AppVersion("kernel")
	if err != nil {
		return err
	}
	stdlib, err := l.AppVersion("stdlib")
	if err != nil {
		return err
	}
	apps := []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
	}
	if selfContained {
		saslV, err := l.AppVersion("sasl")
		if err != nil {
			return err
		}
		apps = append(apps, release.AppVsn{Name: "sasl", Vsn: saslV})
	}
	apps = append(apps, release.AppVsn{Name: appMod, Vsn: vsn})
	relBody := release.RelResource(appMod, vsn, erts, apps)
	if err := os.WriteFile(filepath.Join(relDir, appMod+".rel"), []byte(relBody), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(relDir, "sys.config"), []byte(release.SysConfig(appMod)), 0o644); err != nil {
		return err
	}
	if name == "" {
		name = appMod + "@127.0.0.1"
	}
	if !validNodeName(name) {
		return fmt.Errorf("invalid node name %q (must match name@host)", name)
	}
	if err := os.WriteFile(filepath.Join(relDir, "vm.args"), []byte(release.VmArgs(name)), 0o644); err != nil {
		return err
	}
	man, err := release.Manifest{App: appMod, Vsn: vsn, Node: name}.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "wm.json"), man, 0o644); err != nil {
		return err
	}

	// Generate the boot script with systools. cwd = releases/<vsn> so make_script
	// reads <app>.rel and writes <app>.script/<app>.boot there. `local` bakes the
	// build-time ebin paths so the release boots in place without an install step;
	// self-contained releases drop `local` since they boot from an installed
	// $ROOTDIR/lib tree instead (see scriptOpts below).
	absEbin, err := filepath.Abs(ebin)
	if err != nil {
		return err
	}
	scriptOpts := `local,{path,["%s"]}`
	if selfContained {
		scriptOpts = `{path,["%s"]}` // non-local: paths resolve from $ROOTDIR/lib at boot
	}
	eval := fmt.Sprintf(
		`case systools:make_script("%s",[`+scriptOpts+`]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		appMod, absEbin)
	if res, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
		return fmt.Errorf("systools:make_script failed: %v\n%s", err, res)
	}

	if tar {
		tarOpts := `{path,["%s"]}`
		if selfContained {
			tarOpts = fmt.Sprintf(`{erts,%q},{path,["%%s"]}`, l.OtpLib())
		}
		eval := fmt.Sprintf(
			`case systools:make_tar("%s",[`+tarOpts+`]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
			appMod, absEbin)
		if res, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
			return fmt.Errorf("systools:make_tar failed: %v\n%s", err, res)
		}
	}

	if selfContained {
		if err := assembleTargetSystem(ctx, l, out, relDir, appMod, name, vsn); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "built release %s %s (%s)\n", appMod, vsn, name)
	return nil
}

// assembleTargetSystem turns the make_tar {erts} output into a full standalone
// target system: unpack -> generate start_clean.boot + RELEASES via erl -> write
// the launcher layout -> repack <out>/<app>-<vsn>.tar.gz.
func assembleTargetSystem(ctx context.Context, l erlang.Layout, out, relDir, appMod, node, vsn string) error {
	// Stage the target system under a <app>-<vsn>/ directory so the final tarball
	// unpacks into a single self-named directory (conventional; avoids a tar bomb
	// that explodes bin/, lib/, erts-* loosely into the user's cwd).
	stage := filepath.Join(out, ".target")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	work := filepath.Join(stage, appMod+"-"+vsn)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}

	// Unpack the make_tar output (written as <relDir>/<app>.tar.gz).
	tf, err := os.Open(filepath.Join(relDir, appMod+".tar.gz"))
	if err != nil {
		return err
	}
	err = release.Untar(tf, work)
	tf.Close()
	if err != nil {
		return err
	}

	workRel := filepath.Join(work, "releases", vsn)

	// systools:make_tar does not bundle vm.args (and only conditionally sys.config);
	// bin/start references both, so copy them from the built release into the bundle.
	for _, f := range []string{"sys.config", "vm.args"} {
		data, rerr := os.ReadFile(filepath.Join(relDir, f))
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(filepath.Join(workRel, f), data, 0o644); werr != nil {
			return werr
		}
	}

	// start_clean.boot (kernel+stdlib) for control nodes (bin/stop), non-local.
	erts, err := l.ErtsVersion()
	if err != nil {
		return err
	}
	kernel, err := l.AppVersion("kernel")
	if err != nil {
		return err
	}
	stdlib, err := l.AppVersion("stdlib")
	if err != nil {
		return err
	}
	cleanRel := release.RelResource("start_clean", vsn, erts, []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
	})
	if err := os.WriteFile(filepath.Join(workRel, "start_clean.rel"), []byte(cleanRel), 0o644); err != nil {
		return err
	}
	cleanEval := `case systools:make_script("start_clean",[{path,["../../lib/*/ebin"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`
	if res, err := captureErl(ctx, workRel, l.Erl(), "-noshell", "-eval", cleanEval); err != nil {
		return fmt.Errorf("make_script start_clean failed: %v\n%s", err, res)
	}

	// RELEASES (release_handler), for the canonical target-system layout.
	absWork, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	relEval := fmt.Sprintf(
		`case release_handler:create_RELEASES(%q, %q) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		absWork, filepath.Join(absWork, "releases", vsn, appMod+".rel"))
	if res, err := captureErl(ctx, work, l.Erl(), "-noshell", "-eval", relEval); err != nil {
		return fmt.Errorf("create_RELEASES failed: %v\n%s", err, res)
	}

	// Launcher layout: bin/, start_erl.data, start.boot.
	if err := release.WriteLauncherLayout(work, appMod, node, vsn, erts, filepath.Join(l.OtpLib(), "bin")); err != nil {
		return err
	}

	// Repack the final self-contained artifact. Packing `stage` (not `work`)
	// prefixes every entry with <app>-<vsn>/, so the tarball unpacks into that dir.
	final, err := os.Create(filepath.Join(out, appMod+"-"+vsn+".tar.gz"))
	if err != nil {
		return err
	}
	defer final.Close()
	return release.TarGz(stage, final)
}
