package erlang

import "fmt"

// DefaultVersion is the OTP release verified in docs/verified-sources.md.
const DefaultVersion = "29.0.3"

// SourceURL returns the official OTP source tarball URL for a version.
func SourceURL(version string) string {
	return fmt.Sprintf(
		"https://github.com/erlang/otp/releases/download/OTP-%s/otp_src_%s.tar.gz",
		version, version)
}
