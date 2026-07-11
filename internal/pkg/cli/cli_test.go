package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string // substring; empty means no error
		wantOut string // substring expected in stdout
	}{
		{name: "no args prints usage", args: nil, wantOut: "usage: wm"},
		{name: "known command is stubbed", args: []string{"check"}, wantErr: "not implemented"},
		{name: "unknown command errors", args: []string{"frobnicate"}, wantErr: "unknown command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut strings.Builder
			err := Run(context.Background(), tt.args, strings.NewReader(""), &out, &errOut)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("got err %v, want substring %q", err, tt.wantErr)
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("stdout = %q, want substring %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestErlangList(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".local", "erlang", "29.0.3", "bin"), 0o755)
	os.WriteFile(filepath.Join(home, ".local", "erlang", "29.0.3", "bin", "erl"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(home, ".local", "erlang", "29.0.3", "bin", "erlc"), []byte("x"), 0o755)
	t.Setenv("HOME", home)
	var out strings.Builder
	err := Run(context.Background(), []string{"erlang", "list"}, strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "29.0.3") {
		t.Fatalf("list = %q", out.String())
	}
}

func TestRunAssemblesErlcAndErl(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate: no bin/ left in the package dir
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Main() { }\n"), 0o644)
	t.Setenv("HOME", t.TempDir())
	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()
	var out strings.Builder
	if err := Run(context.Background(), []string{"run", src}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || !strings.Contains(cmds[0], "erlc") || !strings.Contains(cmds[1], "m:main()") {
		t.Fatalf("cmds = %v", cmds)
	}
	if !strings.Contains(out.String(), "booting m") {
		t.Fatalf("stdout = %q, want 'booting m'", out.String())
	}
}

func TestRunRejectsTraversalVersion(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Main() {}\n"), 0o644)
	t.Setenv("HOME", t.TempDir())
	err := Run(context.Background(), []string{"run", src, "--version", "../../etc"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("want error for traversal --version, got nil")
	}
}

func TestBuildCommand(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate: no bin/ left in the package dir
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Serve() {}\n"), 0o644)
	var out strings.Builder
	err := Run(context.Background(), []string{"build", src}, strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	erl := filepath.Join("bin", "m.erl")
	b, err := os.ReadFile(erl)
	if err != nil {
		t.Fatalf("expected %s: %v", erl, err)
	}
	if !strings.Contains(string(b), "-module(m).") {
		t.Fatalf("bad erl:\n%s", b)
	}
}

func TestBuildOutFlagAndCollision(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate: no bin/ left in the package dir
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Serve() {}\n"), 0o644)
	out := t.TempDir()

	// --out writes into the chosen dir
	if err := Run(context.Background(), []string{"build", src, "--out", out}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "m.erl")); err != nil {
		t.Fatalf("expected m.erl in --out dir: %v", err)
	}
	// second build to the same dir collides
	if err := Run(context.Background(), []string{"build", src, "--out", out}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("want collision error on second build to same out, got nil")
	}
}

func TestStartAssemblesDetachedErlAndWritesState(t *testing.T) {
	// Mirror the app fixture shape from testdata/otpapp/go/echoapp/main.go.
	// Read it before t.Chdir below, since the path is relative to the package dir.
	appSrc, err := os.ReadFile("../../../testdata/otpapp/go/echoapp/main.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, appSrc, 0o644)

	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"start", "--out", t.TempDir(), src},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	last := cmds[len(cmds)-1]
	for _, want := range []string{"-detached", "-name echoapp@127.0.0.1", "-setcookie ", "application:start(echoapp)"} {
		if !strings.Contains(last, want) {
			t.Fatalf("erl cmd missing %q:\n%s", want, last)
		}
	}
	st, err := readState("echoapp")
	if err != nil {
		t.Fatalf("state not written: %v", err)
	}
	if st.Node != "echoapp@127.0.0.1" || st.Cookie == "" {
		t.Fatalf("bad state: %+v", st)
	}
}

func TestParseVersionFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantVersion string
		wantRest    []string
		wantErr     bool
	}{
		{"absent", []string{"main.go"}, erlang.DefaultVersion, []string{"main.go"}, false},
		{"space form", []string{"main.go", "--version", "29.0.3"}, "29.0.3", []string{"main.go"}, false},
		{"equals form", []string{"--version=29.0.3", "main.go"}, "29.0.3", []string{"main.go"}, false},
		{"missing value", []string{"main.go", "--version"}, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, rest, err := parseVersionFlag(tt.args)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if v != tt.wantVersion {
				t.Fatalf("version = %q, want %q", v, tt.wantVersion)
			}
			if strings.Join(rest, ",") != strings.Join(tt.wantRest, ",") {
				t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

func TestBuildEmitsAppFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	app := write("app.go", `package echoapp
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echosup"
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
`)
	srv := write("srv.go", `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServer("echo", State{}) }
`)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	err := Run(context.Background(), []string{"build", app, srv, "--out", out, "--vsn", "0.2.3"},
		nil, &buf, &buf)
	if err != nil {
		t.Fatalf("build: %v\n%s", err, buf.String())
	}
	appFile := filepath.Join(out, "echoapp.app")
	data, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("expected %s: %v", appFile, err)
	}
	for _, want := range []string{
		"{application, echoapp,",
		`{vsn, "0.2.3"}`,
		"{modules, [echoapp, echoserver]}",
		"{registered, [echo]}",
		"{mod, {echoapp, []}}",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %s:\n%s", want, appFile, data)
		}
	}
}

func TestBuildAppReturnsAppModule(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte(`package echoapp
import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/otpapp/go/echosup"
)
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop() {}
`), 0o644)
	out := t.TempDir()
	appMod, modules, _, err := buildApp([]string{src}, out)
	if err != nil {
		t.Fatal(err)
	}
	if appMod != "echoapp" {
		t.Fatalf("appMod = %q, want echoapp", appMod)
	}
	if len(modules) != 1 || modules[0] != "echoapp" {
		t.Fatalf("modules = %v", modules)
	}
	if _, err := os.Stat(filepath.Join(out, "echoapp.erl")); err != nil {
		t.Fatalf("echoapp.erl not written: %v", err)
	}
}

func TestBuildSingleFileNoAppFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "srv.go")
	os.WriteFile(p, []byte(`package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
func Start() { otp.StartServer("echo", nil) }
`), 0o644)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	if err := Run(context.Background(), []string{"build", p, "--out", out}, nil, &buf, &buf); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "echoserver.erl")); err != nil {
		t.Fatalf("expected echoserver.erl: %v", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(out, "*.app")); len(entries) != 0 {
		t.Fatalf("no .app expected for a non-application build, got %v", entries)
	}
}

func TestBuildPrintsPartialProgressBeforeError(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate: no bin/ left in the package dir
	dir := t.TempDir()
	out := t.TempDir()

	// Create two distinct modules
	a := filepath.Join(dir, "a.go")
	os.WriteFile(a, []byte("package amod\nfunc Serve() {}\n"), 0o644)
	b := filepath.Join(dir, "b.go")
	os.WriteFile(b, []byte("package bmod\nfunc Serve() {}\n"), 0o644)

	// Pre-create the second module's .erl to force a collision error
	os.WriteFile(filepath.Join(out, "bmod.erl"), []byte("pre-existing"), 0o644)

	var stdout strings.Builder
	err := Run(context.Background(), []string{"build", "--out", out, a, b}, strings.NewReader(""), &stdout, io.Discard)

	// Error must occur due to bmod.erl collision
	if err == nil {
		t.Fatal("expected collision error on second path")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}

	// First path must have been printed before the error
	firstPath := outPath(out, "amod")
	if !strings.Contains(stdout.String(), firstPath) {
		t.Fatalf("first path should have been printed before the error\nstdout = %q\nwant substring: %q", stdout.String(), firstPath)
	}
}

func TestParseStringFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		flag     string
		def      string
		wantVal  string
		wantRest []string
		wantErr  bool
	}{
		{"absent", []string{"foo"}, "--name", "", "", []string{"foo"}, false},
		{"space form", []string{"--name", "x@h"}, "--name", "", "x@h", []string{}, false},
		{"equals form", []string{"--name=x@h"}, "--name", "", "x@h", []string{}, false},
		{"missing value", []string{"--name"}, "--name", "", "", nil, true},
		{"default used", []string{"main.go"}, "--name", "default@host", "default@host", []string{"main.go"}, false},
		{"mixed args space", []string{"foo", "--name", "x@h", "bar"}, "--name", "", "x@h", []string{"foo", "bar"}, false},
		{"mixed args equals", []string{"foo", "--name=x@h", "bar"}, "--name", "", "x@h", []string{"foo", "bar"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, rest, err := parseStringFlag(tt.args, tt.flag, tt.def)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), "requires a value") {
					t.Fatalf("err = %q, want substring 'requires a value'", err.Error())
				}
				return
			}
			if val != tt.wantVal {
				t.Fatalf("val = %q, want %q", val, tt.wantVal)
			}
			if strings.Join(rest, ",") != strings.Join(tt.wantRest, ",") {
				t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

func TestStartNameOverride(t *testing.T) {
	// Read app fixture before t.Chdir, since path is relative to package dir.
	appSrc, err := os.ReadFile("../../../testdata/otpapp/go/echoapp/main.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, appSrc, 0o644)

	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	outDir := t.TempDir()
	if err := Run(context.Background(), []string{"start", "--name", "echo@myhost", "--out", outDir, src},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Assert the last erl command contains the override name
	last := cmds[len(cmds)-1]
	if !strings.Contains(last, "-name echo@myhost") {
		t.Fatalf("erl cmd missing '-name echo@myhost':\n%s", last)
	}
	// Assert it doesn't contain the default
	if strings.Contains(last, "echoapp@127.0.0.1") {
		t.Fatalf("erl cmd should not contain default 'echoapp@127.0.0.1':\n%s", last)
	}

	// Assert state file has the override name
	st, err := readState("echoapp")
	if err != nil {
		t.Fatalf("state not written: %v", err)
	}
	if st.Node != "echo@myhost" {
		t.Fatalf("state.Node = %q, want 'echo@myhost'", st.Node)
	}
}

func TestStopAssemblesRpcAndRemovesState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	var cmds []string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		cmds = append(cmds, name+" "+strings.Join(a, " "))
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"stop", "echoapp"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmds, "\n")
	for _, want := range []string{"-setcookie c0ffee", "rpc:call('echoapp@127.0.0.1', init, stop, [])"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stop cmd missing %q:\n%s", want, joined)
		}
	}
	if _, err := readState("echoapp"); err == nil {
		t.Fatal("state should be removed after stop")
	}
}

func TestStartNoAppModuleErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	// Create a plain package with no App type (no application module)
	os.WriteFile(src, []byte("package plainmod\nfunc Main() {}\n"), 0o644)

	runErl = func(_ context.Context, _, name string, a ...string) error {
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	err := Run(context.Background(), []string{"start", "--out", t.TempDir(), src},
		strings.NewReader(""), &out, io.Discard)

	if err == nil {
		t.Fatal("want error for non-application module, got nil")
	}
	if !strings.Contains(err.Error(), "no application module") {
		t.Fatalf("err = %q, want substring 'no application module'", err.Error())
	}
}

func TestStatusAssemblesPingAndReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	var gotArgs []string
	orig := captureErl
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		gotArgs = a
		return []byte("pong\n"), nil
	}
	defer func() { captureErl = orig }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"status", "echoapp"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "net_adm:ping('echoapp@127.0.0.1')") {
		t.Fatalf("status cmd missing ping:\n%s", joined)
	}
	if !strings.Contains(out.String(), "pong") {
		t.Fatalf("status out = %q", out.String())
	}
}

func TestCallAssemblesGlobalGenServerCall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	orig := captureErl
	var gotArgs []string
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		gotArgs = a
		return []byte("hi\n"), nil
	}
	defer func() { captureErl = orig }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"call", "echo", "hi"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, `gen_server:call({global, echo}, <<"hi">>)`) {
		t.Fatalf("call cmd missing global call:\n%s", joined)
	}
	if strings.TrimSpace(out.String()) != "hi" {
		t.Fatalf("call out = %q", out.String())
	}
}

func TestCallAppOverrideSelectsNamedNode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Write two State-Files to force ambiguity
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "aaa", CodePath: "bin"})
	writeState("other", NodeState{Node: "other@127.0.0.1", Cookie: "bbb", CodePath: "bin"})

	orig := captureErl
	var gotArgs []string
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		gotArgs = a
		return []byte("ok\n"), nil
	}
	defer func() { captureErl = orig }()

	var out strings.Builder
	if err := Run(context.Background(), []string{"call", "--app", "echoapp", "echo", "hi"},
		strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	// Verify the cookie from echoapp's state was used
	if !strings.Contains(joined, "-setcookie aaa") {
		t.Fatalf("call cmd should use echoapp's cookie (-setcookie aaa):\n%s", joined)
	}
	// Verify the gen_server call is there
	if !strings.Contains(joined, `gen_server:call({global, echo}, <<"hi">>)`) {
		t.Fatalf("call cmd missing global call:\n%s", joined)
	}
	if strings.TrimSpace(out.String()) != "ok" {
		t.Fatalf("call out = %q", out.String())
	}
}

func TestCallNoAppMultipleNodesErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Write two State-Files to force ambiguity
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "aaa", CodePath: "bin"})
	writeState("other", NodeState{Node: "other@127.0.0.1", Cookie: "bbb", CodePath: "bin"})

	var out strings.Builder
	err := Run(context.Background(), []string{"call", "echo", "hi"},
		strings.NewReader(""), &out, io.Discard)
	if err == nil {
		t.Fatal("want error for multiple nodes without --app, got nil")
	}
	if !strings.Contains(err.Error(), "multiple running nodes") {
		t.Fatalf("err = %q, want substring 'multiple running nodes'", err.Error())
	}
}

func TestValidAtom(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"plain atom", "echo", true},
		{"underscored atom", "echo_server_1", true},
		{"injection attempt", `x), os:cmd("id")`, false},
		{"uppercase start", "Bad", false},
		{"contains space", "has space", false},
		{"empty", "", false},
		{"leading digit", "1echo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAtom(tt.s); got != tt.want {
				t.Fatalf("validAtom(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestValidNodeName(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"default node name", "echoapp@127.0.0.1", true},
		{"hostname node", "myapp@myhost.example.com", true},
		{"underscored name", "my_app_1@127.0.0.1", true},
		{"injection attempt", `x@h'), os:cmd("id"), rpc:call('x`, false},
		{"missing host", "echoapp@", false},
		{"missing name", "@127.0.0.1", false},
		{"no at sign", "echoapp", false},
		{"contains space", "echo app@127.0.0.1", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validNodeName(tt.s); got != tt.want {
				t.Fatalf("validNodeName(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestCallRejectsInvalidGenServerName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	orig := captureErl
	called := false
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		called = true
		return []byte("should not run"), nil
	}
	defer func() { captureErl = orig }()

	var out strings.Builder
	err := Run(context.Background(), []string{"call", `x), os:cmd("id")`, "hi"},
		strings.NewReader(""), &out, io.Discard)
	if err == nil {
		t.Fatal("want error for invalid gen_server name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid gen_server name") {
		t.Fatalf("err = %q, want substring 'invalid gen_server name'", err.Error())
	}
	if called {
		t.Fatal("erl must not be invoked when the gen_server name fails validation")
	}
}

func TestStartRejectsInvalidNodeName(t *testing.T) {
	appSrc, err := os.ReadFile("../../../testdata/otpapp/go/echoapp/main.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, appSrc, 0o644)

	called := false
	runErl = func(_ context.Context, _, name string, a ...string) error {
		called = true
		return nil
	}
	defer func() { runErl = execRunner }()

	var out strings.Builder
	err = Run(context.Background(), []string{"start", "--name", `x@h'), os:cmd("id`, "--out", t.TempDir(), src},
		strings.NewReader(""), &out, io.Discard)
	if err == nil {
		t.Fatal("want error for invalid --name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid node name") {
		t.Fatalf("err = %q, want substring 'invalid node name'", err.Error())
	}
	if called {
		t.Fatal("erl must not be invoked when --name fails validation")
	}
}

func TestStopRejectsTraversalAppName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	var out strings.Builder
	err := Run(context.Background(), []string{"stop", "../etc/passwd"},
		strings.NewReader(""), &out, io.Discard)
	if err == nil {
		t.Fatal("want error for traversal app name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid app name") {
		t.Fatalf("err = %q, want substring 'invalid app name'", err.Error())
	}
}

func TestStatusRejectsTraversalAppName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var out strings.Builder
	err := Run(context.Background(), []string{"status", "a/b"},
		strings.NewReader(""), &out, io.Discard)
	if err == nil {
		t.Fatal("want error for app name containing a separator, got nil")
	}
	if !strings.Contains(err.Error(), "invalid app name") {
		t.Fatalf("err = %q, want substring 'invalid app name'", err.Error())
	}
}

func TestAttachRejectsTraversalAppName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	err := Run(context.Background(), []string{"attach", ".."},
		strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("want error for '..' app name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid app name") {
		t.Fatalf("err = %q, want substring 'invalid app name'", err.Error())
	}
}

func TestAttachAssemblesRemsh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})

	orig := attachErl
	var gotArgs []string
	attachErl = func(_ context.Context, _, _ string, a ...string) error {
		gotArgs = a
		return nil
	}
	defer func() { attachErl = orig }()

	if err := Run(context.Background(), []string{"attach", "echoapp"},
		strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-remsh echoapp@127.0.0.1", "-setcookie c0ffee"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("attach cmd missing %q:\n%s", want, joined)
		}
	}
}
