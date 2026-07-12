// Package release builds OTP release resources (.rel, sys.config, vm.args) and
// the wm.json manifest. All builders are pure string/JSON functions; the CLI
// wires them and invokes systools via the erl seam.
package release

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AppVsn names an application and its version for the .rel apps list.
type AppVsn struct{ Name, Vsn string }

// RelResource builds an OTP release resource file (.rel) term.
func RelResource(name, vsn, erts string, apps []AppVsn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{release, {%q, %q},\n", name, vsn)
	fmt.Fprintf(&b, " {erts, %q},\n", erts)
	b.WriteString(" [")
	for i, a := range apps {
		if i > 0 {
			b.WriteString("\n  ")
		}
		fmt.Fprintf(&b, "{%s, %q}", a.Name, a.Vsn)
		if i < len(apps)-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("]}.\n")
	return b.String()
}

// SysConfig builds an empty-but-valid sys.config scaffold for app.
func SysConfig(app string) string { return fmt.Sprintf("[{%s, []}].\n", app) }

// VmArgs builds a vm.args carrying only the node name — no cookie (the cookie
// is supplied at boot via a separate 0o600 -args_file overlay).
func VmArgs(node string) string { return fmt.Sprintf("-name %s\n", node) }

// Manifest is the wm.json at a release root: the single source of truth wm start
// reads back to recover app, vsn, and node without parsing vm.args or globbing.
type Manifest struct {
	App  string `json:"app"`
	Vsn  string `json:"vsn"`
	Node string `json:"node"`
}

// Marshal renders the manifest as indented JSON.
func (m Manifest) Marshal() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

// ParseManifest decodes a wm.json manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	err := json.Unmarshal(data, &m)
	return m, err
}

// StartErlData is the releases/start_erl.data content: "<erts-vsn> <rel-vsn>".
func StartErlData(ertsVsn, relVsn string) string {
	return fmt.Sprintf("%s %s\n", ertsVsn, relVsn)
}

// StartScript is a self-locating bin/start: it computes the target root from its
// own path, finds the bundled erts, and boots the release detached. No -setcookie
// (erl uses ~/.erlang.cookie), so the tarball carries no secret.
func StartScript(vsn string) string {
	return `#!/bin/sh
HERE=$(cd "$(dirname "$0")/.." && pwd)
ERTS=$(basename "$HERE"/erts-*)
exec "$HERE/$ERTS/bin/erl" -detached -boot "$HERE/releases/` + vsn + `/start" \
  -config "$HERE/releases/` + vsn + `/sys.config" \
  -args_file "$HERE/releases/` + vsn + `/vm.args"
`
}

// StopScript is a self-locating bin/stop: a short-lived control node (booted with
// the bundled start_clean) that rpc-stops the release node. It shares the node's
// ~/.erlang.cookie automatically on the same host.
func StopScript(node, vsn string) string {
	return `#!/bin/sh
HERE=$(cd "$(dirname "$0")/.." && pwd)
ERTS=$(basename "$HERE"/erts-*)
exec "$HERE/$ERTS/bin/erl" -boot "$HERE/releases/` + vsn + `/start_clean" \
  -name "wmstop_$$@127.0.0.1" -noshell \
  -eval "rpc:call('` + node + `', init, stop, []), init:stop()."
`
}
