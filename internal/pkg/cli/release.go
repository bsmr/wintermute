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
	var filtered []string
	for _, a := range args {
		if a == "--tar" {
			tar = true
			continue
		}
		filtered = append(filtered, a)
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
		return fmt.Errorf("usage: wm release <path>... [--name N] [--out DIR] [--version X] [--vsn V] [--tar]")
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
	if !validAppName(vsn) {
		return fmt.Errorf("invalid vsn %q (must be a safe path segment)", vsn)
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
	relBody := release.RelResource(appMod, vsn, erts, []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
		{Name: appMod, Vsn: vsn},
	})
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
	// build-time ebin paths so the release boots in place without an install step.
	absEbin, err := filepath.Abs(ebin)
	if err != nil {
		return err
	}
	eval := fmt.Sprintf(
		`case systools:make_script("%s",[local,{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
		appMod, absEbin)
	if out, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
		return fmt.Errorf("systools:make_script failed: %v\n%s", err, out)
	}

	if tar {
		eval := fmt.Sprintf(
			`case systools:make_tar("%s",[{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`,
			appMod, absEbin)
		if res, err := captureErl(ctx, relDir, l.Erl(), "-noshell", "-eval", eval); err != nil {
			return fmt.Errorf("systools:make_tar failed: %v\n%s", err, res)
		}
	}

	fmt.Fprintf(stdout, "built release %s %s (%s)\n", appMod, vsn, name)
	return nil
}
