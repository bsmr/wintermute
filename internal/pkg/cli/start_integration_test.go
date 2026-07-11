//go:build integration

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
)

// TestStartBootsAppEndToEnd drives the real `wm start` path — unmocked, against
// the locally installed OTP — to prove the whole start→status→call flow works.
//
// This is the regression guard for the bug where startCmd never emitted
// <app>.app: because the node boots -detached, erl exits 0 regardless of the
// -eval result, so `wm start` printed false success while application:start
// silently failed with {error,{"no such file or directory","echoapp.app"}}. The
// test asserts (a) echoapp.app lands on disk, (b) echoapp shows up in the node's
// running applications, and (c) `wm call echo hello` really reaches the
// {global, echo} gen_server and echoes back "hello".
func TestStartBootsAppEndToEnd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !erlang.NewLayout(home, erlang.DefaultVersion).Installed() {
		t.Skipf("local Erlang %s not installed; run wm erlang install", erlang.DefaultVersion)
	}

	// Isolate the State-File; keep the real HOME so the toolchain resolves.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	out := t.TempDir()
	node := fmt.Sprintf("wmstart_e2e_%d@127.0.0.1", os.Getpid())
	fixtures := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
		"../../../testdata/persistent/go/echoserver/main.go",
	}

	ctx := context.Background()

	// Belt-and-suspenders teardown: tear the node down even if an assertion
	// aborts before the explicit stop below, so no orphan node survives.
	t.Cleanup(func() {
		var sink strings.Builder
		_ = Run(ctx, []string{"stop", "echoapp"}, strings.NewReader(""), &sink, io.Discard)
	})

	// 1. wm start — the real deal: transpile + erlc + detached boot + .app emit.
	startArgs := append([]string{"start", "--name", node, "--out", out}, fixtures...)
	var startOut strings.Builder
	if err := Run(ctx, startArgs, strings.NewReader(""), &startOut, io.Discard); err != nil {
		t.Fatalf("wm start: %v", err)
	}
	appFile := filepath.Join(out, "echoapp.app")
	if _, err := os.Stat(appFile); err != nil {
		t.Fatalf("echoapp.app not emitted at %s: %v", appFile, err)
	}

	// 2. Poll wm status until echoapp appears in the running applications. After
	// a detached boot, application/global registration is not instant.
	var lastStatus string
	running := false
	for i := 0; i < 30; i++ {
		var statusOut strings.Builder
		if err := Run(ctx, []string{"status", "echoapp"}, strings.NewReader(""), &statusOut, io.Discard); err == nil {
			lastStatus = statusOut.String()
			if strings.Contains(lastStatus, "{echoapp,") {
				running = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !running {
		t.Fatalf("echoapp never appeared in running applications (expected {echoapp, in status); last status:\n%s", lastStatus)
	}

	// 3. wm call — proves {global, echo} really registered on the started node.
	var callOut strings.Builder
	if err := Run(ctx, []string{"call", "--app", "echoapp", "echo", "hello"},
		strings.NewReader(""), &callOut, io.Discard); err != nil {
		t.Fatalf("wm call: %v", err)
	}
	if got := strings.TrimSpace(callOut.String()); got != "hello" {
		t.Fatalf("wm call echo hello = %q, want %q", got, "hello")
	}

	// 4. wm stop — the node goes down and the State-File is removed.
	var stopOut strings.Builder
	if err := Run(ctx, []string{"stop", "echoapp"}, strings.NewReader(""), &stopOut, io.Discard); err != nil {
		t.Fatalf("wm stop: %v", err)
	}
	if _, err := statePath("echoapp"); err != nil {
		t.Fatal(err)
	}
	if p, _ := statePath("echoapp"); fileExists(p) {
		t.Fatalf("State-File %s still present after stop", p)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
