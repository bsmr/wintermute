package release

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// failWriter fails every Write, so gzip's flush/Close surfaces an error.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }

var errWrite = errors.New("write failed")

// TestTarGzSurfacesWriteError guards that a flush/close failure is not swallowed
// (a truncated archive must not be reported as a successful release build).
func TestTarGzSurfacesWriteError(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "f"), []byte("data"), 0o644)
	if err := TarGz(src, failWriter{}); err == nil {
		t.Fatal("TarGz to a failing writer returned nil; want an error")
	}
}

func TestTarGzUntarRoundTrip(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0o755)
	os.WriteFile(filepath.Join(src, "bin", "start"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(src, "note.txt"), []byte("hi"), 0o644)

	var buf bytes.Buffer
	if err := TarGz(src, &buf); err != nil {
		t.Fatalf("TarGz: %v", err)
	}
	dst := t.TempDir()
	if err := Untar(&buf, dst); err != nil {
		t.Fatalf("Untar: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dst, "bin", "start"))
	if err != nil {
		t.Fatalf("start missing after round-trip: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("start lost its executable bit: %o", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(filepath.Join(dst, "note.txt"))
	if string(b) != "hi" {
		t.Errorf("note.txt content = %q", b)
	}
}
