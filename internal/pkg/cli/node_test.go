package cli

import (
	"context"
	"os"
	"path/filepath"
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

func TestValidAppName(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"plain app name", "echoapp", true},
		{"underscored app name", "echo_app_1", true},
		{"empty", "", false},
		{"dot-dot", "..", false},
		{"traversal", "../etc/passwd", false},
		{"nested traversal", "../../foo", false},
		{"forward slash", "a/b", false},
		{"backslash", `a\b`, false},
		{"absolute path", "/etc/foo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAppName(tt.s); got != tt.want {
				t.Fatalf("validAppName(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestStatePathRejectsTraversal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, bad := range []string{"../etc/passwd", "a/b", "..", ""} {
		if _, err := statePath(bad); err == nil {
			t.Fatalf("statePath(%q) = nil error, want error", bad)
		}
	}
}

func TestRemoveStateRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	// A sibling file outside the state dir that a traversal could otherwise reach.
	victim := filepath.Join(filepath.Dir(dir), "victim.json")
	os.WriteFile(victim, []byte("{}"), 0o644)
	defer os.Remove(victim)

	if err := removeState("../victim"); err == nil {
		t.Fatal("removeState should reject a traversal app name")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim file must survive a rejected traversal remove: %v", err)
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
