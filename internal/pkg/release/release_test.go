package release

import "testing"

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
