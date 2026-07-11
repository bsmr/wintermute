package erlang

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
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

// maxSourceBytes caps the downloaded tarball size; OTP source is ~64 MiB, so
// 200 MiB leaves generous headroom while still bounding memory use against a
// misbehaving or malicious server.
const maxSourceBytes = 200 << 20

// errSourceTooLarge marks a fetchOnce failure caused by the response body
// exceeding maxSourceBytes. It is as deterministic as a checksum mismatch, so
// fetchSource short-circuits on it instead of retrying.
var errSourceTooLarge = errors.New("source exceeds size ceiling")

// fetchSource downloads url and verifies its SHA-256 equals wantSHA before
// returning the bytes. url is injected (rather than derived internally from a
// version) so tests can point it at an httptest.Server. Transport/HTTP
// failures are retried up to 3 attempts; a checksum mismatch or an oversized
// body is not retried since both are deterministic and retrying them would
// only mask the problem (and, for an oversized body, re-download up to 3x
// the ceiling for nothing).
func fetchSource(ctx context.Context, url, wantSHA string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		data, err := fetchOnce(ctx, client, url)
		if err != nil {
			if errors.Is(err, errSourceTooLarge) {
				return nil, err
			}
			lastErr = err
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != wantSHA {
			return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSHA)
		}
		return data, nil
	}
	return nil, fmt.Errorf("download %s failed after 3 attempts: %w", url, lastErr)
}

// fetchOnce performs a single download attempt, capping the response body at
// maxSourceBytes+1 (via io.LimitReader) so an oversized body is detected by
// length rather than exhausting memory first.
//
// ponytail: ReadAll the whole tarball (~64 MiB) into memory for a one-shot
// install so the hash can be verified before any extraction. Stream-to-temp
// only if memory pressure ever matters.
func fetchOnce(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSourceBytes {
		return nil, fmt.Errorf("source exceeds %d bytes ceiling: %w", maxSourceBytes, errSourceTooLarge)
	}
	return data, nil
}

// tarSupportsStrip reports whether `tar --version` output identifies a tar that
// supports --strip-components (GNU tar or bsdtar). BusyBox tar does not.
func tarSupportsStrip(versionOutput string) bool {
	return strings.Contains(versionOutput, "GNU tar") || strings.Contains(versionOutput, "bsdtar")
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
	want, ok := sourceSHA256[version]
	if !ok {
		return fmt.Errorf("no pinned SHA-256 for OTP version %s; refusing to download unverified source", version)
	}
	l := NewLayout(b.Home, version)
	if err := os.MkdirAll(l.Src, 0o755); err != nil {
		return err
	}
	data, err := fetchSource(ctx, SourceURL(version), want)
	if err != nil {
		return err
	}
	// Check that the system tar supports --strip-components before attempting extraction.
	verOut, err := exec.CommandContext(ctx, "tar", "--version").Output()
	if err != nil {
		return fmt.Errorf("tar not available: %w", err)
	}
	if !tarSupportsStrip(string(verOut)) {
		return fmt.Errorf("system tar does not support --strip-components (need GNU tar or bsdtar)")
	}
	// Extract with system tar for faithful preservation of modification times,
	// symlinks, and file modes. The OTP build is timestamp-driven and the
	// tarball ships symlinks; a hand-rolled extractor that resets mtimes makes
	// make skip regenerating its dependency files and fail on stale paths.
	// ponytail: shell out to tar (a platform tool, not a Go module) instead of
	// re-implementing faithful extraction with archive/tar. --strip-components=1
	// drops the leading otp_src_<ver>/ directory. tar itself rejects absolute
	// and ../ members; the source bytes are SHA-256 verified above.
	tarCmd := exec.CommandContext(ctx, "tar", "xz", "--strip-components=1", "-C", l.Src)
	tarCmd.Stdin = bytes.NewReader(data)
	tarCmd.Stdout = b.Out
	tarCmd.Stderr = b.Out
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("extract %s: %w", SourceURL(version), err)
	}
	return b.Build(ctx, version)
}
