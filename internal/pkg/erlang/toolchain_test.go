package erlang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalled(t *testing.T) {
	home := t.TempDir()
	l := NewLayout(home, "29.0.3")
	if l.Installed() {
		t.Fatal("should not be installed on empty dir")
	}
	if err := os.MkdirAll(l.Bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Erl(), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !l.Installed() {
		t.Fatal("should be installed after writing erl")
	}
	if l.Erlc() != filepath.Join(l.Bin, "erlc") {
		t.Fatalf("Erlc = %q", l.Erlc())
	}
}
