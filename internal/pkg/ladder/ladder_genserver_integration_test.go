//go:build integration

package ladder

import (
	"path/filepath"
	"testing"
)

// Rung III proves gen_server interchangeability on a single node: runEcho boots
// echoserver:start() (which starts the gen_server, registered locally as echo)
// then echoclient:main() (which gen_server:call's it), all in one BEAM node.

func TestRungIII1_ErlangToErlang(t *testing.T) {
	got := runEcho(t,
		filepath.FromSlash("../../../testdata/genserver/erlang/echoserver.erl"),
		filepath.FromSlash("../../../testdata/genserver/erlang/echoclient.erl"))
	if got != "hello" {
		t.Fatalf("rung III.1 = %q, want %q", got, "hello")
	}
}

func TestRungIII2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/genserver/go/echoclient/main.go", dir)
	got := runEcho(t, "../../../testdata/genserver/erlang/echoserver.erl", client)
	if got != "hello" {
		t.Fatalf("rung III.2 = %q", got)
	}
}

func TestRungIII3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/genserver/go/echoserver/main.go", dir)
	got := runEcho(t, server, "../../../testdata/genserver/erlang/echoclient.erl")
	if got != "hello" {
		t.Fatalf("rung III.3 = %q", got)
	}
}

func TestRungIII4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/genserver/go/echoserver/main.go", dir)
	client := transpileToErl(t, "../../../testdata/genserver/go/echoclient/main.go", dir)
	got := runEcho(t, server, client)
	if got != "hello" {
		t.Fatalf("rung III.4 = %q", got)
	}
}
