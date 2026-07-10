package otp

import "testing"

func TestMarkersPanicNatively(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Self() to panic natively (transpile-only marker)")
		}
	}()
	_ = Self()
}
