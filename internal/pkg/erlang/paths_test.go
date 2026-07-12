package erlang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLayout(t *testing.T) {
	l := NewLayout("/home/u", "29.0.3")
	if l.Root != filepath.FromSlash("/home/u/.local/erlang/29.0.3") {
		t.Fatalf("Root = %q", l.Root)
	}
	if l.Src != filepath.Join(l.Root, "src") {
		t.Fatalf("Src = %q", l.Src)
	}
	if l.Bin != filepath.Join(l.Root, "bin") {
		t.Fatalf("Bin = %q", l.Bin)
	}
}

func fakeOtp(t *testing.T) Layout {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib", "erlang")
	for _, d := range []string{"erts-17.0.3", "lib/kernel-11.0.3", "lib/stdlib-8.0.2"} {
		if err := os.MkdirAll(filepath.Join(lib, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Layout{Root: root, Bin: filepath.Join(root, "bin")}
}

func TestErtsVersion(t *testing.T) {
	l := fakeOtp(t)
	got, err := l.ErtsVersion()
	if err != nil || got != "17.0.3" {
		t.Fatalf("ErtsVersion() = %q, %v; want 17.0.3", got, err)
	}
}

func TestAppVersion(t *testing.T) {
	l := fakeOtp(t)
	for name, want := range map[string]string{"kernel": "11.0.3", "stdlib": "8.0.2"} {
		got, err := l.AppVersion(name)
		if err != nil || got != want {
			t.Fatalf("AppVersion(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestAppVersionMissing(t *testing.T) {
	l := fakeOtp(t)
	if _, err := l.AppVersion("nosuch"); err == nil {
		t.Fatal("AppVersion(nosuch) should error")
	}
}
