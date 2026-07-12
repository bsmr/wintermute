//go:build integration

package ladder

import (
	"os"
	"path/filepath"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// TestRungVIII_NativeErlModule builds a supervised release whose echo server is a
// hand-written .erl module (record + guard) while the app and supervisor are
// transpiled Go, boots it, and has the transpiled-Go echoclient call {global,
// echo}. Proves a native module drops into a Wintermute release and interoperates
// with transpiled Go at the supervised-release level.
func TestRungVIII_NativeErlModule(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := mixedNativeApp(t, dir)
	got := runRelease(t, 8, "0.2.7", erls, appFile)
	if got != "hello" {
		t.Fatalf("rung VIII = %q, want hello", got)
	}
}

// mixedNativeApp transpiles the persistent Go app + supervisor into <dir>, copies
// the native echoserver.erl in, and generates echoapp.app listing all three
// modules. Mirrors transpilePersistentApp but swaps the Go server for the native
// module. Returns the .erl paths and the .app path.
func mixedNativeApp(t *testing.T, dir string) ([]string, string) {
	t.Helper()
	goFiles := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
	}
	modules := []string{}
	var registered []string
	var appMod string
	var erls []string
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
	// Native server, copied through as-is.
	nativeSrc, err := os.ReadFile("../../../testdata/native/echoserver.erl")
	if err != nil {
		t.Fatal(err)
	}
	nativeDst := filepath.Join(dir, "echoserver.erl")
	if err := os.WriteFile(nativeDst, nativeSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	erls = append(erls, nativeDst)
	modules = append(modules, "echoserver")

	appFile := filepath.Join(dir, appMod+".app")
	body := transpile.AppResource(appMod, "0.2.7", modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return erls, appFile
}
