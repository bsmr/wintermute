package otp

import (
	"strings"
	"testing"
)

func TestMarkersPanicNatively(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Self() to panic natively (transpile-only marker)")
		}
	}()
	_ = Self()
}

func TestMarkerMessageNamesFix(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "wm build") || !strings.Contains(msg, "Spawn") {
			t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
		}
	}()
	_ = Spawn(func() {})
}
