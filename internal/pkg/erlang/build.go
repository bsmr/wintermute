package erlang

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// Runner executes an external command in dir. Injected for testability.
type Runner func(ctx context.Context, dir, name string, args ...string) error

// Builder builds a local Erlang from source into ~/.local/erlang/<ver>.
type Builder struct {
	Home string
	Out  io.Writer
	Run  Runner
}

// Build runs configure/make/make install in the extracted source tree.
// Download + extraction is done by Task 6's real Runner before this;
// Build only assembles and drives the compile/install steps.
func (b Builder) Build(ctx context.Context, version string) error {
	l := NewLayout(b.Home, version)
	if err := b.Run(ctx, l.Src, "./configure", "--prefix="+l.Root); err != nil {
		return err
	}
	if err := b.Run(ctx, l.Src, "make"); err != nil {
		return err
	}
	return b.Run(ctx, l.Src, "make", "install")
}

// Provision downloads and extracts the OTP source, then builds+installs it.
// It fails fast if the required build tools are missing, so a multi-minute
// build never starts only to die partway through.
func (b Builder) Provision(ctx context.Context, version string) error {
	if missing := MissingBuildTools(exec.LookPath); len(missing) > 0 {
		return fmt.Errorf("missing build tools: %s; also install the ncurses and "+
			"openssl development headers (e.g. libncurses-dev, libssl-dev)",
			strings.Join(missing, ", "))
	}
	l := NewLayout(b.Home, version)
	if err := os.MkdirAll(l.Src, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SourceURL(version), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", SourceURL(version), resp.StatusCode)
	}
	// Extract with system tar for faithful preservation of modification times,
	// symlinks, and file modes. The OTP build is timestamp-driven and the
	// tarball ships symlinks; a hand-rolled extractor that resets mtimes makes
	// make skip regenerating its dependency files and fail on stale paths.
	// ponytail: shell out to tar (a platform tool, not a Go module) instead of
	// re-implementing faithful extraction with archive/tar. --strip-components=1
	// drops the leading otp_src_<ver>/ directory. tar itself rejects absolute
	// and ../ members; the source URL is the pinned official HTTPS release.
	tarCmd := exec.CommandContext(ctx, "tar", "xz", "--strip-components=1", "-C", l.Src)
	tarCmd.Stdin = resp.Body
	tarCmd.Stdout = b.Out
	tarCmd.Stderr = b.Out
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("extract %s: %w", SourceURL(version), err)
	}
	return b.Build(ctx, version)
}
