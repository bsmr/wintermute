package erlang

import (
	"fmt"
	"regexp"
)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ValidateVersion rejects any version string that is not exactly N.N.N. This
// blocks path traversal into filepath.Join(... version ...) via NewLayout.
func ValidateVersion(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("invalid version %q: must be N.N.N", v)
	}
	return nil
}
