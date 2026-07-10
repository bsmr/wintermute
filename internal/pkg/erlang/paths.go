package erlang

import "path/filepath"

// Layout is the on-disk structure for one installed Erlang version.
type Layout struct {
	Root string // ~/.local/erlang/<version>
	Src  string // retained sources for debugging
	Bin  string // installed erl/erlc (configure --prefix target)
}

// NewLayout builds the layout for a version under home.
func NewLayout(home, version string) Layout {
	root := filepath.Join(home, ".local", "erlang", version)
	return Layout{Root: root, Src: filepath.Join(root, "src"), Bin: filepath.Join(root, "bin")}
}
