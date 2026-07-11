package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// validAppName rejects empty names and anything that could escape the state
// directory once joined into statePath (path separators or "..").
func validAppName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\") && !strings.Contains(s, "..")
}

// NodeState is the persisted identity of a running wm node, keyed by app name.
type NodeState struct {
	Node     string `json:"node"`
	Cookie   string `json:"cookie"`
	CodePath string `json:"codepath"`
}

// stateDir is $XDG_STATE_HOME/wintermute, defaulting to ~/.local/state/wintermute.
func stateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "wintermute"), nil
}

// statePath resolves the State-File path for app. Validating app here (rather
// than at each call site) covers read/write/remove with a single guard, since
// all three go through statePath.
func statePath(app string) (string, error) {
	if !validAppName(app) {
		return "", fmt.Errorf("invalid app name %q", app)
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app+".json"), nil
}

func writeState(app string, s NodeState) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p, err := statePath(app)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return nil
}

func readState(app string) (NodeState, error) {
	p, err := statePath(app)
	if err != nil {
		return NodeState{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return NodeState{}, fmt.Errorf("no running node for %q; run wm start", app)
	}
	var s NodeState
	if err := json.Unmarshal(data, &s); err != nil {
		return NodeState{}, fmt.Errorf("corrupt state for %q: %w", app, err)
	}
	return s, nil
}

func removeState(app string) error {
	p, err := statePath(app)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// newCookie returns 16 cryptographically-random bytes, hex-encoded.
func newCookie() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// captureErl runs a command and returns its combined output. Overridable in
// tests to assert assembled command lines without executing erl.
var captureErl = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	return c.CombinedOutput()
}

// attachErl runs an interactive command wired to the real terminal (used by
// `wm attach` for erl -remsh). Overridable in tests.
var attachErl = func(ctx context.Context, dir, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
