package erlang

import (
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
