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

// sourceSHA256 pins the SHA-256 of each supported OTP source tarball. Verified
// before every build (stdlib crypto/sha256; no third-party sigstore verifier).
var sourceSHA256 = map[string]string{
	"29.0.3": "f920c660b16794bcb7270d1cbf680f7747c719650bcd6ac449508a32c2a8972a",
}
