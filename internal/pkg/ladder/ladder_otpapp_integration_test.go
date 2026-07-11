//go:build integration

package ladder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// runOtpApp compiles the app/sup/server .erl files, places the .app on the code
// path, boots application:start(echoapp), runs the client, and halts.
func runOtpApp(t *testing.T, serverErls []string, appFile, clientErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	for _, src := range append(append([]string{}, serverErls...), clientErl) {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}
	data, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "echoapp.app"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	eval := "application:start(echoapp), echoclient:main(), init:stop()."
	out, err := exec.Command(l.Erl(), "-noshell", "-pa", work, "-eval", eval).CombinedOutput()
	if err != nil {
		t.Fatalf("erl: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// transpileApp transpiles the three server-side Go fixtures into <dir> and
// generates echoapp.app there, returning the .erl paths and the .app path.
func transpileApp(t *testing.T, dir string) ([]string, string) {
	t.Helper()
	goFiles := []string{
		"../../../testdata/otpapp/go/echoapp/main.go",
		"../../../testdata/otpapp/go/echosup/main.go",
		"../../../testdata/otpapp/go/echoserver/main.go",
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
	body := transpile.AppResource(appMod, "0.2.3", modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return erls, appFile
}

func erlServer() []string {
	return []string{
		"../../../testdata/otpapp/erlang/echoapp.erl",
		"../../../testdata/otpapp/erlang/echosup.erl",
		"../../../testdata/otpapp/erlang/echoserver.erl",
	}
}

const erlAppFile = "../../../testdata/otpapp/erlang/echoapp.app"
const erlClient = "../../../testdata/otpapp/erlang/echoclient.erl"

func TestRungIV1_ErlangToErlang(t *testing.T) {
	got := runOtpApp(t, erlServer(), erlAppFile, erlClient)
	if got != "hello" {
		t.Fatalf("rung IV.1 = %q, want hello", got)
	}
}

func TestRungIV2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/otpapp/go/echoclient/main.go", dir)
	got := runOtpApp(t, erlServer(), erlAppFile, client)
	if got != "hello" {
		t.Fatalf("rung IV.2 = %q, want hello", got)
	}
}

func TestRungIV3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpileApp(t, dir)
	got := runOtpApp(t, erls, appFile, erlClient)
	if got != "hello" {
		t.Fatalf("rung IV.3 = %q, want hello", got)
	}
}

func TestRungIV4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpileApp(t, dir)
	client := transpileToErl(t, "../../../testdata/otpapp/go/echoclient/main.go", dir)
	got := runOtpApp(t, erls, appFile, client)
	if got != "hello" {
		t.Fatalf("rung IV.4 = %q, want hello", got)
	}
}
