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
