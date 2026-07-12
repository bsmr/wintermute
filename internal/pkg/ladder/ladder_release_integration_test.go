//go:build integration

package ladder

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
)

// releaseCookie is the shared distribution cookie for all rung-VI nodes. In a
// real `wm start` the cookie is random and lives in a 0o600 run-file; the ladder
// uses a fixed value so the control caller can connect, but still proves the
// off-argv mechanism: the app node receives it via -args_file, never on argv.
const releaseCookie = "wm_release"

// buildEchoRelease builds a formal OTP release for echoapp under work:
// lib/echoapp-<vsn>/ebin/{*.beam,echoapp.app} + releases/<vsn>/{echoapp.rel,
// sys.config,vm.args,echoapp.boot,echoapp.script}. The modules come from the
// given .erl sources (hand-written or Wintermute-transpiled). It returns the
// releases/<vsn> dir and the app node name written into vm.args.
func buildEchoRelease(t *testing.T, l erlang.Layout, work string, idx int, vsn string, erls []string, appFile string) (relDir, appNode string) {
	t.Helper()

	ebin := filepath.Join(work, "lib", "echoapp-"+vsn, "ebin")
	if err := os.MkdirAll(ebin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, src := range erls {
		if out, err := exec.Command(l.Erlc(), "-o", ebin, src).CombinedOutput(); err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}
	appData, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ebin, "echoapp.app"), appData, 0o644); err != nil {
		t.Fatal(err)
	}

	relDir = filepath.Join(work, "releases", vsn)
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		t.Fatal(err)
	}
	erts, err := l.ErtsVersion()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := l.AppVersion("kernel")
	if err != nil {
		t.Fatal(err)
	}
	stdlib, err := l.AppVersion("stdlib")
	if err != nil {
		t.Fatal(err)
	}
	appNode = fmt.Sprintf("echorel_v%d_%d@127.0.0.1", idx, os.Getpid())

	relBody := release.RelResource("echoapp", vsn, erts, []release.AppVsn{
		{Name: "kernel", Vsn: kernel},
		{Name: "stdlib", Vsn: stdlib},
		{Name: "echoapp", Vsn: vsn},
	})
	if err := os.WriteFile(filepath.Join(relDir, "echoapp.rel"), []byte(relBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relDir, "sys.config"), []byte(release.SysConfig("echoapp")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relDir, "vm.args"), []byte(release.VmArgs(appNode)), 0o644); err != nil {
		t.Fatal(err)
	}

	absEbin, err := filepath.Abs(ebin)
	if err != nil {
		t.Fatal(err)
	}
	makeScript := exec.Command(l.Erl(), "-noshell", "-eval",
		fmt.Sprintf(`case systools:make_script("echoapp",[local,{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`, absEbin))
	makeScript.Dir = relDir
	if out, err := makeScript.CombinedOutput(); err != nil {
		t.Fatalf("systools:make_script: %v\n%s", err, out)
	}
	return relDir, appNode
}

// bootReleaseAndCall boots the release at relDir/echoapp detached (the boot
// script starts kernel+stdlib+echoapp itself — no -eval application:start),
// supplying the cookie via a 0o600 -args_file overlay (never on argv), then runs
// echoclient:main/0 on a separate control node that converges the global registry
// and calls {global, echo}. Returns the caller's trimmed stdout.
func bootReleaseAndCall(t *testing.T, l erlang.Layout, work, relDir, appNode string, idx int) string {
	t.Helper()

	cookieFile := filepath.Join(work, "echoapp.vmargs")
	if err := os.WriteFile(cookieFile, []byte("-setcookie "+releaseCookie+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	boot := exec.Command(l.Erl(), "-detached",
		"-boot", filepath.Join(relDir, "echoapp"),
		"-config", filepath.Join(relDir, "sys.config"),
		"-args_file", filepath.Join(relDir, "vm.args"),
		"-args_file", cookieFile)
	if out, err := boot.CombinedOutput(); err != nil {
		t.Fatalf("boot release node: %v\n%s", err, out)
	}
	stopNode := fmt.Sprintf("echorelstop_v%d_%d@127.0.0.1", idx, os.Getpid())
	t.Cleanup(func() {
		stop := exec.Command(l.Erl(),
			"-name", stopNode, "-setcookie", releaseCookie, "-noshell",
			"-eval", fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", appNode))
		_ = stop.Run()
	})

	clientDir := t.TempDir()
	if out, err := exec.Command(l.Erlc(), "-o", clientDir, erlPersistClient).CombinedOutput(); err != nil {
		t.Fatalf("erlc client: %v\n%s", err, out)
	}
	ctrlNode := fmt.Sprintf("echorelctrl_v%d_%d@127.0.0.1", idx, os.Getpid())
	callerEval := fmt.Sprintf(
		`Wait = fun Loop(0) -> erlang:error(global_echo_timeout); `+
			`Loop(N) -> net_adm:ping('%s'), global:sync(), `+
			`case global:whereis_name(echo) of `+
			`undefined -> timer:sleep(100), Loop(N - 1); _ -> ok end end, `+
			`Wait(30), echoclient:main(), init:stop().`, appNode)
	caller := exec.Command(l.Erl(),
		"-name", ctrlNode, "-setcookie", releaseCookie, "-noshell",
		"-pa", clientDir, "-eval", callerEval)
	out, err := caller.CombinedOutput()
	if err != nil {
		t.Fatalf("caller node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// runRelease builds a release from the given sources, boots it, and returns the
// cross-node caller reply. idx makes node names unique per rung.
func runRelease(t *testing.T, idx int, vsn string, erls []string, appFile string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	relDir, appNode := buildEchoRelease(t, l, work, idx, vsn, erls, appFile)
	return bootReleaseAndCall(t, l, work, relDir, appNode, idx)
}

// VI.1: release built from hand-written Erlang modules.
func TestRungVI1_ErlangRelease(t *testing.T) {
	got := runRelease(t, 1, "0.2.3", erlPersistServer(), erlPersistAppFile)
	if got != "hello" {
		t.Fatalf("rung VI.1 = %q, want hello", got)
	}
}

// VI.2: release built from Wintermute-transpiled Go modules.
func TestRungVI2_WintermuteRelease(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := transpilePersistentApp(t, dir)
	got := runRelease(t, 2, "0.2.4", erls, appFile)
	if got != "hello" {
		t.Fatalf("rung VI.2 = %q, want hello", got)
	}
}

// VI tarball check: `--tar`-equivalent make_tar produces a self-consistent
// release archive that, unpacked on a same-version host, boots and serves the
// same reply. make_tar's own boot script assumes an install/$ROOT context, so we
// regenerate a `local` boot script on the unpacked tree — this proves the tar
// PAYLOAD (echoapp modules + .rel + resources) is complete and bootable in place.
func TestRungVI_Tarball(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	vsn := "0.2.4"
	work := t.TempDir()
	erls, appFile := transpilePersistentApp(t, t.TempDir())
	relDir, _ := buildEchoRelease(t, l, work, 3, vsn, erls, appFile)

	// make_tar packages the release (no {erts,_} — runs against installed OTP).
	absEbin := filepath.Join(work, "lib", "echoapp-"+vsn, "ebin")
	makeTar := exec.Command(l.Erl(), "-noshell", "-eval",
		fmt.Sprintf(`case systools:make_tar("echoapp",[{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`, absEbin))
	makeTar.Dir = relDir
	if out, err := makeTar.CombinedOutput(); err != nil {
		t.Fatalf("systools:make_tar: %v\n%s", err, out)
	}
	tarPath := filepath.Join(relDir, "echoapp.tar.gz")

	// Unpack into a fresh dir and assert the expected payload is present.
	unpack := t.TempDir()
	untar(t, tarPath, unpack)
	for _, want := range []string{
		filepath.Join("lib", "echoapp-"+vsn, "ebin", "echoapp.beam"),
		filepath.Join("releases", vsn, "echoapp.rel"),
	} {
		if _, err := os.Stat(filepath.Join(unpack, want)); err != nil {
			t.Fatalf("tarball missing %s: %v", want, err)
		}
	}

	// Regenerate a local boot script on the unpacked tree, then boot it.
	unpackEbin, _ := filepath.Abs(filepath.Join(unpack, "lib", "echoapp-"+vsn, "ebin"))
	unpackRel := filepath.Join(unpack, "releases", vsn)
	appNode := fmt.Sprintf("echorel_v%d_%d@127.0.0.1", 4, os.Getpid())
	if err := os.WriteFile(filepath.Join(unpackRel, "vm.args"), []byte(release.VmArgs(appNode)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unpackRel, "sys.config"), []byte(release.SysConfig("echoapp")), 0o644); err != nil {
		t.Fatal(err)
	}
	regen := exec.Command(l.Erl(), "-noshell", "-eval",
		fmt.Sprintf(`case systools:make_script("echoapp",[local,{path,["%s"]}]) of ok -> halt(0); E -> io:format("~p~n",[E]), halt(1) end`, unpackEbin))
	regen.Dir = unpackRel
	if out, err := regen.CombinedOutput(); err != nil {
		t.Fatalf("regen make_script on unpacked release: %v\n%s", err, out)
	}
	got := bootReleaseAndCall(t, l, unpack, unpackRel, appNode, 4)
	if got != "hello" {
		t.Fatalf("tarball release = %q, want hello", got)
	}
}

// untar extracts a .tar.gz into dst (flat structure preserved). Test-only,
// trusts systools-produced archives (no path-traversal hardening needed).
func untar(t *testing.T, archive, dst string) {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dst, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := os.Create(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // test-only, trusted archive
				out.Close()
				t.Fatal(err)
			}
			out.Close()
		}
	}
}
