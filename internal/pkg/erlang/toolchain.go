package erlang

import (
	"os"
	"path/filepath"
)

func (l Layout) Erlc() string { return filepath.Join(l.Bin, "erlc") }
func (l Layout) Erl() string  { return filepath.Join(l.Bin, "erl") }

// Installed reports whether erl exists in this layout's bin.
func (l Layout) Installed() bool {
	info, err := os.Stat(l.Erl())
	return err == nil && !info.IsDir()
}
