//go:build integration

package erlang

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestProvisionRealBuild(t *testing.T) {
	home := t.TempDir()
	b := Builder{
		Home: home,
		Out:  os.Stderr,
		Run: func(ctx context.Context, dir, name string, args ...string) error {
			c := exec.CommandContext(ctx, name, args...)
			c.Dir, c.Stdout, c.Stderr = dir, os.Stderr, os.Stderr
			return c.Run()
		},
	}
	if err := b.Provision(context.Background(), DefaultVersion); err != nil {
		t.Fatal(err)
	}
	if !NewLayout(home, DefaultVersion).Installed() {
		t.Fatal("erl not installed after Provision")
	}
}
