package erlang

import "testing"

func TestSourceURL(t *testing.T) {
	got := SourceURL("29.0.3")
	want := "https://github.com/erlang/otp/releases/download/OTP-29.0.3/otp_src_29.0.3.tar.gz"
	if got != want {
		t.Fatalf("SourceURL = %q, want %q", got, want)
	}
}
