package release

import (
	"os"
	"path/filepath"
)

// WriteLauncherLayout turns an unpacked make_tar bundle (erts-*/, lib/,
// releases/<vsn>/) into a full target system: a top-level bin/ with the erts
// launchers plus a self-locating bin/start and bin/stop, releases/start_erl.data,
// and releases/<vsn>/start.boot (a copy of <app>.boot).
func WriteLauncherLayout(root, app, node, vsn, ertsVsn, otpBinDir string) error {
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		data, err := os.ReadFile(filepath.Join(otpBinDir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(binDir, name), data, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, "start"), []byte(StartScript(vsn)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(binDir, "stop"), []byte(StopScript(node, vsn)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "releases", "start_erl.data"),
		[]byte(StartErlData(ertsVsn, vsn)), 0o644); err != nil {
		return err
	}
	// systools:make_tar already names the release boot start.boot inside the
	// bundle; only synthesise it from <app>.boot when it is absent (e.g. a bundle
	// built without make_tar's naming).
	startBoot := filepath.Join(root, "releases", vsn, "start.boot")
	if _, err := os.Stat(startBoot); os.IsNotExist(err) {
		boot, rerr := os.ReadFile(filepath.Join(root, "releases", vsn, app+".boot"))
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(startBoot, boot, 0o644); werr != nil {
			return werr
		}
	}
	return nil
}
