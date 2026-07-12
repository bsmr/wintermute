package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLauncherLayout(t *testing.T) {
	root := t.TempDir()
	// Fake unpacked bundle bits the layout depends on.
	os.MkdirAll(filepath.Join(root, "releases", "0.2.6"), 0o755)
	os.WriteFile(filepath.Join(root, "releases", "0.2.6", "echo.boot"), []byte("BOOT"), 0o644)
	// Fake OTP bin/ source.
	otpBin := t.TempDir()
	for _, f := range []string{"erl", "epmd", "run_erl", "to_erl"} {
		os.WriteFile(filepath.Join(otpBin, f), []byte("launcher"), 0o755)
	}

	if err := WriteLauncherLayout(root, "echo", "echo@127.0.0.1", "0.2.6", "17.0.3", otpBin); err != nil {
		t.Fatalf("WriteLauncherLayout: %v", err)
	}

	// start/stop present and executable.
	for _, f := range []string{"bin/start", "bin/stop", "bin/erl"} {
		fi, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s not executable: %o", f, fi.Mode().Perm())
		}
	}
	// start_erl.data + start.boot.
	sed, _ := os.ReadFile(filepath.Join(root, "releases", "start_erl.data"))
	if string(sed) != "17.0.3 0.2.6\n" {
		t.Errorf("start_erl.data = %q", sed)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "0.2.6", "start.boot")); err != nil {
		t.Errorf("start.boot not created: %v", err)
	}
}
