package erlang

import (
	"fmt"
	"path/filepath"
	"strings"
)

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

// OtpLib is the OTP tree root: $PREFIX/lib/erlang, holding erts-<v> and lib/.
func (l Layout) OtpLib() string { return filepath.Join(l.Root, "lib", "erlang") }

// globVersion returns the single version suffix of dir/<prefix>-*, erroring on
// zero or multiple matches (an ambiguous install is a real problem, not a guess).
func globVersion(dir, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+"-*"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one %s-* under %s, found %d", prefix, dir, len(matches))
	}
	return strings.TrimPrefix(filepath.Base(matches[0]), prefix+"-"), nil
}

// ErtsVersion reads the erts version from OtpLib/erts-<v>.
func (l Layout) ErtsVersion() (string, error) { return globVersion(l.OtpLib(), "erts") }

// AppVersion reads an OTP application's version from OtpLib/lib/<name>-<v>.
func (l Layout) AppVersion(name string) (string, error) {
	return globVersion(filepath.Join(l.OtpLib(), "lib"), name)
}
