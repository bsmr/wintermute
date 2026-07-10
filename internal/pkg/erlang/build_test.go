package erlang

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestBuildAssemblesConfigureMakeInstall(t *testing.T) {
	var cmds []string
	b := Builder{
		Home: "/home/u",
		Out:  io.Discard,
		Run: func(_ context.Context, dir, name string, args ...string) error {
			cmd := dir + "|" + name
			if len(args) > 0 {
				cmd += " " + strings.Join(args, " ")
			}
			cmds = append(cmds, cmd)
			return nil
		},
	}
	if err := b.Build(context.Background(), "29.0.3"); err != nil {
		t.Fatal(err)
	}
	src := "/home/u/.local/erlang/29.0.3/src"
	want := []string{
		src + "|./configure --prefix=/home/u/.local/erlang/29.0.3",
		src + "|make",
		src + "|make install",
	}
	if len(cmds) != len(want) {
		t.Fatalf("commands = %v", cmds)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Fatalf("cmd[%d] = %q, want %q", i, cmds[i], want[i])
		}
	}
}
