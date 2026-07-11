package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	want := NodeState{Node: "echoapp@127.0.0.1", Cookie: "deadbeef", CodePath: "/tmp/work"}
	if err := writeState("echoapp", want); err != nil {
		t.Fatal(err)
	}
	got, err := readState("echoapp")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if err := removeState("echoapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := readState("echoapp"); err == nil {
		t.Fatal("expected error after remove")
	} else if !strings.Contains(err.Error(), "wm start") {
		t.Fatalf("error should suggest wm start, got %v", err)
	}
}

func TestRemoveStateIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := removeState("nope"); err != nil {
		t.Fatalf("removing absent state should be nil, got %v", err)
	}
}

func TestNewCookieUnique(t *testing.T) {
	a, _ := newCookie()
	b, _ := newCookie()
	if a == "" || a == b {
		t.Fatalf("cookies must be non-empty and unique: %q %q", a, b)
	}
}

func TestWriteStateIsOwnerOnly(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := writeState("echoapp", NodeState{Node: "n", Cookie: "c", CodePath: "p"}); err != nil {
		t.Fatal(err)
	}
	p, _ := statePath("echoapp")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o, want 600 (contains the node cookie)", fi.Mode().Perm())
	}
}

func TestCaptureErlDefaultRunsCommand(t *testing.T) {
	out, err := captureErl(context.Background(), ".", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("captureErl = %q", out)
	}
}
