package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	os.Remove(filepath.Join("bin", "m.erl"))
}

func TestBuildCommand(t *testing.T) {
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
	os.Remove(erl)
}
