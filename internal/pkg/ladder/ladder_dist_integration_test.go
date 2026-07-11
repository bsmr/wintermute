//go:build integration

package ladder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
)

// runEchoDist compiles the two .erl files, boots a server node (which stays
// alive) and a client node that connects to it, and returns the client's
// trimmed stdout. The two nodes run on this host via -sname, connected over
// localhost through epmd with a dedicated cookie; net_adm:ping + global:sync
// converge the global registry so global:whereis_name(echo) resolves the remote
// server Pid. Node names carry the test PID so concurrent/leftover runs don't
// collide. The server node has no init:stop; it is killed after the client run.
func runEchoDist(t *testing.T, serverErl, clientErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	for _, src := range []string{serverErl, clientErl} {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("hostname: %v", err)
	}
	host = strings.Split(host, ".")[0] // short hostname for -sname
	pid := os.Getpid()
	serverName := fmt.Sprintf("wm_echo_server_%d", pid)
	clientName := fmt.Sprintf("wm_echo_client_%d", pid)
	serverNode := serverName + "@" + host
	readyFile := filepath.Join(work, "server_ready")

	// Server node: register echo globally, signal readiness, then stay alive.
	// The file:write_file marker fires only after global:register_name returns,
	// so the client never races ahead of registration. This stays in the -eval
	// (orchestration), keeping the fixture code node-name- and sync-free.
	serverEval := fmt.Sprintf(`echoserver:start(), file:write_file("%s", <<"ok">>)`, readyFile)
	server := exec.Command(l.Erl(),
		"-sname", serverName, "-setcookie", "wm_test",
		"-noshell", "-pa", work, "-eval", serverEval)
	if err := server.Start(); err != nil {
		t.Fatalf("start server node: %v", err)
	}
	defer func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	}()

	// Wait for the server-ready marker (registration done) before booting the client.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server node did not become ready within 15s")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Client node: connect, converge global, run the echo, stop.
	clientEval := fmt.Sprintf(
		`net_adm:ping('%s'), global:sync(), echoclient:main(), init:stop().`, serverNode)
	client := exec.Command(l.Erl(),
		"-sname", clientName, "-setcookie", "wm_test",
		"-noshell", "-pa", work, "-eval", clientEval)
	out, err := client.CombinedOutput()
	if err != nil {
		t.Fatalf("client node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRungII1_ErlangToErlang(t *testing.T) {
	got := runEchoDist(t,
		filepath.FromSlash("../../../testdata/echo-dist/erlang/echoserver.erl"),
		filepath.FromSlash("../../../testdata/echo-dist/erlang/echoclient.erl"))
	if got != "hello" {
		t.Fatalf("rung II.1 echo = %q, want %q", got, "hello")
	}
}

func TestRungII2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/echo-dist/go/echoclient/main.go", dir)
	got := runEchoDist(t, "../../../testdata/echo-dist/erlang/echoserver.erl", client)
	if got != "hello" {
		t.Fatalf("rung II.2 echo = %q", got)
	}
}

func TestRungII3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo-dist/go/echoserver/main.go", dir)
	got := runEchoDist(t, server, "../../../testdata/echo-dist/erlang/echoclient.erl")
	if got != "hello" {
		t.Fatalf("rung II.3 echo = %q", got)
	}
}

func TestRungII4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo-dist/go/echoserver/main.go", dir)
	client := transpileToErl(t, "../../../testdata/echo-dist/go/echoclient/main.go", dir)
	got := runEchoDist(t, server, client)
	if got != "hello" {
		t.Fatalf("rung II.4 echo = %q", got)
	}
}
