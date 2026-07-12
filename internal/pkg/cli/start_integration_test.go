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

// TestReleaseStartCallStopEndToEnd drives the real two-step 0.2.5 flow — unmocked,
// against the locally installed OTP: `wm release` builds a formal OTP release,
// `wm start <dir>` boots it from the generated boot script (no -eval), and
// status/call/stop drive it over Distributed Erlang.
//
// It asserts (a) `wm release` emits wm.json + the boot script on disk, (b) after
// `wm start <dir>` echoapp shows up in the node's running applications, (c)
// `wm call --app echoapp echo hello` reaches the {global, echo} gen_server and
// echoes "hello", and (d) `wm stop` removes the State-File. The cookie reaches
// the node via a 0o600 -args_file run-file, never on argv.
func TestReleaseStartCallStopEndToEnd(t *testing.T) {
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
	vsn := "0.2.5"
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

	// 1. wm release — transpile + erlc into the lib layout, emit resources, and
	// generate the boot script via systools. --name pins a unique node name into
	// wm.json so repeated runs never collide on epmd.
	releaseArgs := append([]string{"release", "--out", out, "--vsn", vsn, "--name", node}, fixtures...)
	var releaseOut strings.Builder
	if err := Run(ctx, releaseArgs, strings.NewReader(""), &releaseOut, io.Discard); err != nil {
		t.Fatalf("wm release: %v", err)
	}
	for _, want := range []string{
		"wm.json",
		filepath.Join("releases", vsn, "echoapp.boot"),
	} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Fatalf("wm release did not emit %s: %v", want, err)
		}
	}

	// 2. wm start <release-dir> — boot the finished release detached.
	var startOut strings.Builder
	if err := Run(ctx, []string{"start", out}, strings.NewReader(""), &startOut, io.Discard); err != nil {
		t.Fatalf("wm start: %v", err)
	}

	// 3. Poll wm status until echoapp appears in the running applications. After
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

	// 4. wm call — proves {global, echo} really registered on the started node.
	var callOut strings.Builder
	if err := Run(ctx, []string{"call", "--app", "echoapp", "echo", "hello"},
		strings.NewReader(""), &callOut, io.Discard); err != nil {
		t.Fatalf("wm call: %v", err)
	}
	if got := strings.TrimSpace(callOut.String()); got != "hello" {
		t.Fatalf("wm call echo hello = %q, want %q", got, "hello")
	}

	// 5. wm stop — the node goes down and the State-File is removed.
	var stopOut strings.Builder
	if err := Run(ctx, []string{"stop", "echoapp"}, strings.NewReader(""), &stopOut, io.Discard); err != nil {
		t.Fatalf("wm stop: %v", err)
	}
	p, err := statePath("echoapp")
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(p) {
		t.Fatalf("State-File %s still present after stop", p)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
