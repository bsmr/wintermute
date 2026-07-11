package erlang

import "testing"

func TestValidateVersion(t *testing.T) {
	for _, ok := range []string{"29.0.3", "0.0.0", "27.1.10"} {
		if err := ValidateVersion(ok); err != nil {
			t.Fatalf("ValidateVersion(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"../../etc", "29.0", "29.0.3/../x", "", "v29.0.3", "29.0.3-rc1"} {
		if err := ValidateVersion(bad); err == nil {
			t.Fatalf("ValidateVersion(%q) = nil, want error", bad)
		}
	}
}
