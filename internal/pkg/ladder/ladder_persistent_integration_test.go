//go:build integration

package ladder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// persistCookie is the shared distribution cookie for all rung-V nodes.
const persistCookie = "wm_persist"

// runPersistent proves cross-node persistent-node interchangeability. It boots a
// DETACHED application node that hosts echoapp (which registers {global, echo}),
// then runs the caller (echoclient:main/0) on a SEPARATE control node that
// net_kernel-connects to the app node and resolves the global name. The reply is
// asserted by the caller returning trimmed stdout "hello".
//
// idx makes every node name unique per rung so parallel/repeated runs never
// collide on epmd. The control caller node polls net_adm:ping + global:sync +
// global:whereis_name(echo) in a bounded retry loop before calling: after a
// detached boot the global name registration is NOT instant, so without the poll
// the cross-node gen_server:call races the registration and flakes.
func runPersistent(t *testing.T, idx int, serverErls []string, appFile, callerErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()

	// Compile the app modules and the caller into the shared code path.
	for _, src := range append(append([]string{}, serverErls...), callerErl) {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}

	// Place echoapp.app on the code path.
	data, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "echoapp.app"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	pid := os.Getpid()
	appNode := fmt.Sprintf("echoapp_v%d_%d@127.0.0.1", idx, pid)
	ctrlNode := fmt.Sprintf("echoctrl_v%d_%d@127.0.0.1", idx, pid)
	stopNode := fmt.Sprintf("echostop_v%d_%d@127.0.0.1", idx, pid)

	// Boot the application node detached; it stays alive hosting {global, echo}.
	boot := exec.Command(l.Erl(),
		"-detached", "-name", appNode, "-setcookie", persistCookie,
		"-pa", work, "-eval", "application:start(echoapp)")
	if out, err := boot.CombinedOutput(); err != nil {
		t.Fatalf("boot detached app node: %v\n%s", err, out)
	}

	// Tear the detached node down after the test so no orphan survives a run.
	t.Cleanup(func() {
		stop := exec.Command(l.Erl(),
			"-name", stopNode, "-setcookie", persistCookie, "-noshell",
			"-eval", fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", appNode))
		_ = stop.Run()
	})

	// Control caller node: connect, converge the global registry, wait until the
	// global echo name resolves (bounded retry), then run the caller and stop.
	callerEval := fmt.Sprintf(
		`Wait = fun Loop(0) -> erlang:error(global_echo_timeout); `+
			`Loop(N) -> net_adm:ping('%s'), global:sync(), `+
			`case global:whereis_name(echo) of `+
			`undefined -> timer:sleep(100), Loop(N - 1); _ -> ok end end, `+
			`Wait(30), echoclient:main(), init:stop().`, appNode)
	caller := exec.Command(l.Erl(),
		"-name", ctrlNode, "-setcookie", persistCookie, "-noshell",
		"-pa", work, "-eval", callerEval)
	out, err := caller.CombinedOutput()
	if err != nil {
		t.Fatalf("caller node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// transpilePersistentApp transpiles the three persistent server-side Go fixtures
// into <dir> and generates echoapp.app there, returning the .erl paths and the
// .app path. Mirrors transpileApp but targets the persistent testdata.
func transpilePersistentApp(t *testing.T, dir string) ([]string, string) {
	t.Helper()
	goFiles := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
		"../../../testdata/persistent/go/echoserver/main.go",
	}
	var erls, modules, registered []string
	var appMod string
	for _, gf := range goFiles {
		src, err := os.ReadFile(gf)
		if err != nil {
			t.Fatal(err)
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, r.Module+".erl")
		if err := os.WriteFile(p, []byte(r.Erl), 0o644); err != nil {
			t.Fatal(err)
		}
		erls = append(erls, p)
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			appMod = r.Module
		}
	}
	appFile := filepath.Join(dir, appMod+".app")
	body := transpile.AppResource(appMod, "0.2.4", modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return erls, appFile
}

func erlPersistServer() []string {
	return []string{
		"../../../testdata/persistent/erlang/echoapp.erl",
		"../../../testdata/persistent/erlang/echosup.erl",
		"../../../testdata/persistent/erlang/echoserver.erl",
	}
}

const erlPersistAppFile = "../../../testdata/persistent/erlang/echoapp.app"
const erlPersistClient = "../../../testdata/persistent/erlang/echoclient.erl"
const goPersistClient = "../../../testdata/persistent/go/echoclient/main.go"

// V.1: Erlang app + Erlang caller.
func TestRungV1_ErlangToErlang(t *testing.T) {
	got := runPersistent(t, 1, erlPersistServer(), erlPersistAppFile, erlPersistClient)
	if got != "hello" {
		t.Fatalf("rung V.1 = %q, want hello", got)
	}
}

// V.2: Erlang app + Wintermute (transpiled Go) caller.
func TestRungV2_WintermuteCaller(t *testing.T) {
	dir := t.TempDir()
	caller := transpileToErl(t, goPersistClient, dir)
	got := runPersistent(t, 2, erlPersistServer(), erlPersistAppFile, caller)
	if got != "hello" {
		t.Fatalf("rung V.2 = %q, want hello", got)
	}
}

// V.3: Wintermute app + Erlang caller.
func TestRungV3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpilePersistentApp(t, dir)
	got := runPersistent(t, 3, erls, appFile, erlPersistClient)
	if got != "hello" {
		t.Fatalf("rung V.3 = %q, want hello", got)
	}
}

// V.4: Wintermute app + Wintermute caller.
func TestRungV4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpilePersistentApp(t, dir)
	caller := transpileToErl(t, goPersistClient, dir)
	got := runPersistent(t, 4, erls, appFile, caller)
	if got != "hello" {
		t.Fatalf("rung V.4 = %q, want hello", got)
	}
}
