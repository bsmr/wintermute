package release

import (
	"strings"
	"testing"
)

func TestRelResource(t *testing.T) {
	got := RelResource("echo", "0.2.5", "17.0.3", []AppVsn{
		{"kernel", "11.0.3"}, {"stdlib", "8.0.2"}, {"echo", "0.2.5"},
	})
	want := `{release, {"echo", "0.2.5"},
 {erts, "17.0.3"},
 [{kernel, "11.0.3"},
  {stdlib, "8.0.2"},
  {echo, "0.2.5"}]}.
`
	if got != want {
		t.Fatalf("RelResource mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestSysConfig(t *testing.T) {
	if got := SysConfig("echo"); got != "[{echo, []}].\n" {
		t.Fatalf("SysConfig = %q", got)
	}
}

func TestVmArgs(t *testing.T) {
	if got := VmArgs("echo@127.0.0.1"); got != "-name echo@127.0.0.1\n" {
		t.Fatalf("VmArgs = %q", got)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	m := Manifest{App: "echo", Vsn: "0.2.5", Node: "echo@127.0.0.1"}
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(data)
	if err != nil || got != m {
		t.Fatalf("round-trip = %+v, %v; want %+v", got, err, m)
	}
}

func TestParseManifestBad(t *testing.T) {
	if _, err := ParseManifest([]byte("{not json")); err == nil {
		t.Fatal("ParseManifest should error on bad JSON")
	}
}

func TestStartErlData(t *testing.T) {
	if got := StartErlData("17.0.3", "0.2.6"); got != "17.0.3 0.2.6\n" {
		t.Fatalf("StartErlData = %q", got)
	}
}

func TestStartScript(t *testing.T) {
	s := StartScript("0.2.6")
	for _, want := range []string{
		"#!/bin/sh",
		`HERE=$(cd "$(dirname "$0")/.." && pwd)`,
		`erts-*`,
		"-detached",
		"releases/0.2.6/start",
		"releases/0.2.6/sys.config",
		"releases/0.2.6/vm.args",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("StartScript missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "-setcookie") {
		t.Errorf("StartScript must not set a cookie (uses ~/.erlang.cookie):\n%s", s)
	}
}

func TestStopScript(t *testing.T) {
	s := StopScript("echo@127.0.0.1", "0.2.6")
	for _, want := range []string{
		"#!/bin/sh",
		"releases/0.2.6/start_clean",
		"rpc:call('echo@127.0.0.1', init, stop, [])",
		"halt(1)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("StopScript missing %q:\n%s", want, s)
		}
	}
}
