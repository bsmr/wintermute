# Wintermute 0.2.1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the echo across two connected BEAM nodes — transpiled Wintermute is interchangeable with hand-written Erlang cross-node — by adding `global`-registry discovery to the transpiler and a two-node ladder (step II).

**Architecture:** Minimal extension of the existing packages. Two new transpile-only markers (`otp.RegisterGlobal`/`otp.WhereisGlobal`) map to `global:register_name`/`global:whereis_name` — the only change vs the 0.1.0 echo, since Pids are already location-transparent. Four distributed fixtures mirror the 0.1.0 echoes with discovery swapped. A test-driven `runEchoDist` helper boots two same-host `-sname` nodes and four gated rungs prove interchangeability.

**Tech Stack:** Go stdlib only (`go/ast`, `os/exec`, `os`, `testing`, `time`); Erlang/OTP 29.0.3 (`global`, `net_kernel`, `epmd`, `net_adm`).

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute`.
- **Stdlib only.** No third-party modules.
- **Never-silent-wrong.** The transpiler errors on anything outside its subset.
- **main() → run()**; all logic in `internal/pkg/`.
- **VERSION = `0.2.1`** (already bumped and committed).
- **Cross-node addressing → `global` registry** (locked). Same-host two nodes via `-sname` (locked). Ladder orchestration is test-driven, not a production helper (locked).
- **Commit style:** conventional commits; sign off (`git commit -s`); trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Suite stays green:** `go test ./...` after every task. The step-II integration rungs (`go test -tags integration ./internal/pkg/ladder/`) must pass on the provisioned OTP 29.0.3 before merge; existing single-node rungs 1–4 stay green.

---

## Task 1: `otp.RegisterGlobal` / `otp.WhereisGlobal` markers

**Files:**
- Modify: `pkg/otp/otp.go`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: `otp.RegisterGlobal(name string, p Pid)` and `otp.WhereisGlobal(name string) Pid` — transpile-only markers that panic natively via the existing `transpileOnly` helper.

- [ ] **Step 1: Write the failing test**

Add to `pkg/otp/otp_test.go`:

```go
func TestGlobalMarkersPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"RegisterGlobal", func() { RegisterGlobal("echo", Pid{}) }},
		{"WhereisGlobal", func() { _ = WhereisGlobal("echo") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if !strings.Contains(msg, "wm build") || !strings.Contains(msg, tc.name) {
					t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
				}
			}()
			tc.call()
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/ -run TestGlobalMarkersPanic -v`
Expected: FAIL — `RegisterGlobal`/`WhereisGlobal` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `pkg/otp/otp.go`, after the `Whereis` marker:

```go
func RegisterGlobal(name string, p Pid) { transpileOnly("RegisterGlobal") }              // -> global:register_name(name, Pid)
func WhereisGlobal(name string) Pid      { transpileOnly("WhereisGlobal"); return Pid{} } // -> global:whereis_name(name)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/otp/ -v`
Expected: PASS (existing panic/message tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/
git commit -s -m "feat(otp): add RegisterGlobal/WhereisGlobal markers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Transpile `RegisterGlobal`/`WhereisGlobal` to `global:*`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitCall`'s `switch sel.Sel.Name`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File(src string) (string, string, error)`.
- Produces: `emitCall` handles `RegisterGlobal` → `global:register_name(<atom>, <pid>)` and `WhereisGlobal` → `global:whereis_name(<atom>)`, modeled on the existing `Register`/`Whereis` cases (atom name via `unquoteAtom`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestFile_RegisterGlobal(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Serve() {}
func Start() { otp.RegisterGlobal("echo", otp.Spawn(Serve)) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "global:register_name(echo, spawn(fun ?MODULE:serve/0))") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFile_WhereisGlobal(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.Send(otp.WhereisGlobal("echo"), otp.Self()) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "global:whereis_name(echo) ! self()") {
		t.Fatalf("got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TestFile_(RegisterGlobal|WhereisGlobal)' -v`
Expected: FAIL — `emitCall` hits `default` → "unsupported otp call: RegisterGlobal".

- [ ] **Step 3: Write minimal implementation**

In `emitCall`'s `switch sel.Sel.Name`, add two cases right after the existing `case "Whereis":` (which uses `fmt.Sprintf("whereis(%s)", unquoteAtom(args[0]))`):

```go
	case "RegisterGlobal":
		return fmt.Sprintf("global:register_name(%s, %s)", unquoteAtom(args[0]), args[1]), nil
	case "WhereisGlobal":
		return fmt.Sprintf("global:whereis_name(%s)", unquoteAtom(args[0])), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (existing tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): map RegisterGlobal/WhereisGlobal to global:*

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Distributed fixtures + golden transpile tests

**Files:**
- Create: `testdata/echo-dist/go/echoserver/main.go`
- Create: `testdata/echo-dist/go/echoclient/main.go`
- Create: `testdata/echo-dist/erlang/echoserver.erl`
- Create: `testdata/echo-dist/erlang/echoclient.erl`
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File` with `RegisterGlobal`/`WhereisGlobal` (Task 2), `otp` markers (Task 1).
- Produces: the four fixtures later tasks (ladder rungs) compile and run.

- [ ] **Step 1: Write the failing golden tests**

Add to `internal/pkg/transpile/transpile_test.go`:

```go
func TestFile_GoldenDistServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/echo-dist/go/echoserver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-module(echoserver).",
		"receive\n        {echo, From, Text} ->",
		"From ! {ok, Text}",
		"start() -> global:register_name(echo, spawn(fun ?MODULE:serve/0)).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GoldenDistClient(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/echo-dist/go/echoclient/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "global:whereis_name(echo) ! {echo, self(), <<\"hello\">>}") {
		t.Fatalf("missing global:whereis_name send in:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TestFile_GoldenDist' -v`
Expected: FAIL — fixture files do not exist (`os.ReadFile` error).

- [ ] **Step 3: Create the fixtures**

`testdata/echo-dist/go/echoserver/main.go`:

```go
package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}

func Start() { otp.RegisterGlobal("echo", otp.Spawn(Serve)) }
```

`testdata/echo-dist/go/echoclient/main.go`:

```go
package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Main() {
	otp.Send(otp.WhereisGlobal("echo"), Echo{From: otp.Self(), Text: "hello"})
	reply := otp.Receive().(Ok)
	otp.Print(reply.Text)
}
```

`testdata/echo-dist/erlang/echoserver.erl`:

```erlang
-module(echoserver).
-export([serve/0, start/0]).

serve() ->
    receive
        {echo, From, Text} -> From ! {ok, Text}, serve()
    end.

start() -> global:register_name(echo, spawn(fun echoserver:serve/0)).
```

`testdata/echo-dist/erlang/echoclient.erl`:

```erlang
-module(echoclient).
-export([main/0]).

main() ->
    global:whereis_name(echo) ! {echo, self(), <<"hello">>},
    receive {ok, Text} -> io:format("~s~n", [Text]) end.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'TestFile_GoldenDist' -v`
Expected: PASS — both fixtures transpile to the expected `global:*` Erlang.

- [ ] **Step 5: Commit**

```bash
git add testdata/echo-dist/ internal/pkg/transpile/
git commit -s -m "test(transpile): add distributed echo fixtures + golden tests

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: `runEchoDist` helper + rung II.1 (erl ↔ erl)

**Files:**
- Create: `internal/pkg/ladder/ladder_dist_integration_test.go`
- Test: same file (gated `//go:build integration`)

**Interfaces:**
- Consumes: `erlang.NewLayout`, `erlang.DefaultVersion`, `Layout.Installed()`, `Layout.Erlc()`, `Layout.Erl()`; the `transpileToErl` helper already in `internal/pkg/ladder/ladder_integration_test.go` (same package). The dist fixtures from Task 3.
- Produces: `runEchoDist(t *testing.T, serverErl, clientErl string) string` — compiles both `.erl`, boots two `-sname` nodes, returns the client's trimmed stdout. Later rungs (Task 5) call it.

- [ ] **Step 1: Write the failing test (rung II.1)**

Create `internal/pkg/ladder/ladder_dist_integration_test.go`:

```go
//go:build integration

package ladder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
)

// runEchoDist compiles the two .erl files, boots a server node (which stays
// alive) and a client node that connects to it, and returns the client's
// trimmed stdout. The two nodes run on this host via -sname, connected over
// localhost through epmd with a dedicated cookie; net_adm:ping + global:sync
// converge the global registry so global:whereis_name(echo) resolves the remote
// server Pid. Node names carry the test PID so concurrent/leftover runs don't
// collide. The server node has no init:stop; it is killed after the client run.
func runEchoDist(t *testing.T, serverErl, clientErl string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	work := t.TempDir()
	for _, src := range []string{serverErl, clientErl} {
		out, err := exec.Command(l.Erlc(), "-o", work, src).CombinedOutput()
		if err != nil {
			t.Fatalf("erlc %s: %v\n%s", src, err, out)
		}
	}

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("hostname: %v", err)
	}
	host = strings.Split(host, ".")[0] // short hostname for -sname
	pid := os.Getpid()
	serverName := fmt.Sprintf("wm_echo_server_%d", pid)
	clientName := fmt.Sprintf("wm_echo_client_%d", pid)
	serverNode := serverName + "@" + host
	readyFile := filepath.Join(work, "server_ready")

	// Server node: register echo globally, signal readiness, then stay alive.
	// The file:write_file marker fires only after global:register_name returns,
	// so the client never races ahead of registration. This stays in the -eval
	// (orchestration), keeping the fixture code node-name- and sync-free.
	serverEval := fmt.Sprintf(`echoserver:start(), file:write_file("%s", <<"ok">>)`, readyFile)
	server := exec.Command(l.Erl(),
		"-sname", serverName, "-setcookie", "wm_test",
		"-noshell", "-pa", work, "-eval", serverEval)
	if err := server.Start(); err != nil {
		t.Fatalf("start server node: %v", err)
	}
	defer func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	}()

	// Wait for the server-ready marker (registration done) before booting the client.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server node did not become ready within 15s")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Client node: connect, converge global, run the echo, stop.
	clientEval := fmt.Sprintf(
		`net_adm:ping('%s'), global:sync(), echoclient:main(), init:stop().`, serverNode)
	client := exec.Command(l.Erl(),
		"-sname", clientName, "-setcookie", "wm_test",
		"-noshell", "-pa", work, "-eval", clientEval)
	out, err := client.CombinedOutput()
	if err != nil {
		t.Fatalf("client node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRungII1_ErlangToErlang(t *testing.T) {
	got := runEchoDist(t,
		filepath.FromSlash("../../../testdata/echo-dist/erlang/echoserver.erl"),
		filepath.FromSlash("../../../testdata/echo-dist/erlang/echoclient.erl"))
	if got != "hello" {
		t.Fatalf("rung II.1 echo = %q, want %q", got, "hello")
	}
}
```

- [ ] **Step 2: Run the test to verify it passes on real Erlang**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungII1 -v`
Expected: PASS (`hello`) on the provisioned OTP 29.0.3. If local Erlang is absent, the test SKIPs — provision first (`./bin/wm erlang install`).

(No RED-then-GREEN here: the helper and the test land together, and the test is the first exercise of a two-node boot. Confirm it genuinely runs — not skipped — by checking the output shows `PASS` not `SKIP`.)

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/ladder/ladder_dist_integration_test.go
git commit -s -m "test(ladder): two-node runEchoDist helper + rung II.1 (erl<->erl)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Rungs II.2 – II.4 (Wintermute sides)

**Files:**
- Modify: `internal/pkg/ladder/ladder_dist_integration_test.go`
- Test: same file

**Interfaces:**
- Consumes: `runEchoDist` (Task 4); `transpileToErl(t, goPath, dir string) string` from `ladder_integration_test.go` (same package); the dist fixtures (Task 3).

- [ ] **Step 1: Write the failing tests (three rungs)**

Append to `internal/pkg/ladder/ladder_dist_integration_test.go`:

```go
func TestRungII2_WintermuteClient(t *testing.T) {
	dir := t.TempDir()
	client := transpileToErl(t, "../../../testdata/echo-dist/go/echoclient/main.go", dir)
	got := runEchoDist(t, "../../../testdata/echo-dist/erlang/echoserver.erl", client)
	if got != "hello" {
		t.Fatalf("rung II.2 echo = %q", got)
	}
}

func TestRungII3_WintermuteServer(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo-dist/go/echoserver/main.go", dir)
	got := runEchoDist(t, server, "../../../testdata/echo-dist/erlang/echoclient.erl")
	if got != "hello" {
		t.Fatalf("rung II.3 echo = %q", got)
	}
}

func TestRungII4_BothWintermute(t *testing.T) {
	dir := t.TempDir()
	server := transpileToErl(t, "../../../testdata/echo-dist/go/echoserver/main.go", dir)
	client := transpileToErl(t, "../../../testdata/echo-dist/go/echoclient/main.go", dir)
	got := runEchoDist(t, server, client)
	if got != "hello" {
		t.Fatalf("rung II.4 echo = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they pass on real Erlang**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungII -v`
Expected: PASS for II.1–II.4 (each `hello`) on OTP 29.0.3. Confirm they RUN (not SKIP).

Note: `transpileToErl` writes `<dir>/echoserver.erl` / `<dir>/echoclient.erl` from the fixtures' `package` names; `runEchoDist` compiles them into its own `work` dir and boots `echoserver:start()` / `echoclient:main()`. The module names match by construction.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/ladder/ladder_dist_integration_test.go
git commit -s -m "test(ladder): rungs II.2-II.4 (Wintermute client/server/both)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Verification gate

**Files:** none (verification only; fixes land as follow-up commits if the gate finds issues).

- [ ] **Step 1: Full unit suite + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: all green (transpile golden dist tests, otp global-marker tests included).

- [ ] **Step 2: Real step-II integration ladder** (memory: green units are not enough)

```bash
go test -tags integration ./internal/pkg/ladder/ -v
```
Expected: single-node rungs 1–4 AND distributed rungs II.1–II.4 all PASS on real OTP 29.0.3. This is the distributed-interop thesis proof. If any rung SKIPs, provision Erlang first (`./bin/wm erlang install`).

- [ ] **Step 3: Security tooling** (unchanged surface, but confirm no regressions)

```bash
govulncheck ./...
gosec ./...
gitleaks detect
```
Expected: no new findings beyond the 0.2.0-triaged dual-use patterns. Triage/fix anything new.

- [ ] **Step 4: Update HANDOVER.md**

Mark 0.2.1 complete; note the next 0.2.x step (`wm run` two-node orchestration, then `gen_server`). Commit.

---

## Self-Review

**Spec coverage:** §1 new markers → T1 (otp) + T2 (transpile); §2 fixtures → T3; §3 orchestration (`runEchoDist`, `-sname`, `wm_test` cookie, `net_adm:ping`+`global:sync`, server lifecycle/kill) → T4; §4 rungs II.1–II.4 → T4 (II.1) + T5 (II.2–II.4); testing (unit + gated integration) → T1–T3 (unit), T4–T5 (integration), T6 (gate). All spec sections covered.

**Placeholder scan:** No TBD/TODO. The `%s`/`%d` in `fmt.Sprintf` are real format verbs (readyFile path, node PID, node name), not placeholders. Fixture and helper code is complete and runnable.

**Type consistency:** `RegisterGlobal(name string, p Pid)` / `WhereisGlobal(name string) Pid` are defined in T1 and consumed identically by T2's Erlang mapping and T3's fixtures. `runEchoDist(t, serverErl, clientErl) string` is defined in T4 and called with the same signature in T4/T5. `transpileToErl(t, goPath, dir) string` matches the existing helper in `ladder_integration_test.go`.

**Ordering note:** T1 (markers) precedes T2 (transpile needs the markers to exist for its test src to parse — though the parser doesn't resolve imports, keeping them ordered is clearest). T3's golden tests need T1+T2. T4 introduces `runEchoDist`; T5 reuses it. T4/T5 need real Erlang; they SKIP without it, so the gate (T6 Step 2) is where the thesis is actually proven.
