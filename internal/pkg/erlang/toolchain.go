package erlang

import (
	"os"
	"path/filepath"
)

func (l Layout) Erlc() string { return filepath.Join(l.Bin, "erlc") }
func (l Layout) Erl() string  { return filepath.Join(l.Bin, "erl") }

// Installed reports whether both erl and erlc exist in this layout's bin.
func (l Layout) Installed() bool {
	for _, p := range []string{l.Erl(), l.Erlc()} {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
