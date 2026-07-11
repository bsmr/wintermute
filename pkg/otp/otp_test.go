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

func TestGlobalMarkersPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"RegisterGlobal", func() { RegisterGlobal("echo", Pid{}) }},
		{"WhereisGlobal", func() { _ = WhereisGlobal("echo") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if !strings.Contains(msg, "wm build") || !strings.Contains(msg, tc.name) {
					t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
				}
			}()
			tc.call()
		})
	}
}

func TestStartSupervisorPanics(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "wm build") || !strings.Contains(msg, "StartSupervisor") {
			t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
		}
	}()
	_ = StartSupervisor(nil)
}

func TestChildIsPlainData(t *testing.T) {
	c := Child{ID: "echo", Start: func() {}}
	if c.ID != "echo" || c.Start == nil {
		t.Fatal("Child should hold an ID and a Start func without panicking")
	}
}

func TestGenServerMarkersPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"StartServer", func() { StartServer("echo", nil) }},
		{"Call", func() { _ = Call("echo", "hi") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if !strings.Contains(msg, "wm build") || !strings.Contains(msg, tc.name) {
					t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
				}
			}()
			tc.call()
		})
	}
}

func TestStartServerGlobalPanicsNatively(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StartServerGlobal did not panic")
		}
	}()
	StartServerGlobal("echo", struct{}{})
}

func TestCallGlobalPanicsNatively(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("CallGlobal did not panic")
		}
	}()
	CallGlobal("echo", "hi")
}
