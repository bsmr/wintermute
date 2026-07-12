# Wintermute 0.2.7 — Native Erlang interop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `wm build`/`wm release` accept hand-written `.erl` modules as inputs alongside Go, compiling and packaging them natively so a project can escape the transpiler subset when OTP needs constructs Go cannot express.

**Architecture:** One integration point — `buildApp` in `internal/pkg/cli/cli.go`, shared by `wm build` and `wm release`. A `.erl` input bypasses the transpiler, is validated (basename = module name), copied through to the stage dir (existing overwrite-guard catches collisions), and listed in `modules`; downstream `erlc` + `.app`/`.rel` generation is unchanged. Go↔native interop uses existing OTP mechanisms (registered gen_server + `otp.CallGlobal`); no new marker.

**Tech Stack:** Go (stdlib only), Erlang/OTP 29.0.3 toolchain (`erlc`, `systools`), Go `testing` with a `//go:build integration` tag for real-toolchain tests.

## Global Constraints

- **Stdlib only.** No third-party Go modules. (project `CLAUDE.md`)
- **TDD, red → green.** Write the failing test, watch it fail, then implement. (project `CLAUDE.md`)
- **main() → run() pattern.** All logic in `internal/pkg/`; `main()` is a thin wrapper. (project `CLAUDE.md`)
- **Build output to `bin/` only:** `go build -o bin/wm ./cmd/wm`. Never bare `go build`. (project `CLAUDE.md`)
- **Module path is `go.muehmer.eu/wintermute`**, not the GitHub path.
- **No transpiler change, no `pkg/otp` change** — 0.2.7 is CLI/fixtures only.
- **Native `.erl` modules are non-application** by scope: app + supervisor stay transpiled Go. `appMod` detection is untouched.
- **Module name = file basename**; Erlang's `erlc` guarantees `-module(x)` matches `x.erl`. Validate with `validAppName` (rejects `/`, `\`, `..`).
- **Version:** 0.2.7. Branch: `development-0.2.7-work`.
- **Language:** replies to the user in German; all code/comments/commits in English.

---

### Task 1: Route `.erl` inputs through `buildApp`

The core change. Both `wm build` and `wm release` funnel through `buildApp`, so both gain native support here.

**Files:**
- Modify: `internal/pkg/cli/cli.go:151-178` (`buildApp`)
- Test: `internal/pkg/cli/cli_test.go` (append)

**Interfaces:**
- Consumes: `validAppName(s string) bool` (`internal/pkg/cli/node.go:17`), `outPath(dir, mod string) string` (`internal/pkg/cli/cli.go:577`).
- Produces: `buildApp` now accepts paths ending in `.erl`; for each it appends the basename-derived module name to the returned `modules`, copies the file to `outPath(out, mod)`, sets neither `appMod` nor `registered`. Signature unchanged: `buildApp(paths []string, out string) (appMod string, modules, registered []string, err error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/cli/cli_test.go`:

```go
func TestBuildAppAcceptsNativeErl(t *testing.T) {
	src := t.TempDir()
	erl := filepath.Join(src, "greeting.erl")
	os.WriteFile(erl, []byte("-module(greeting).\n-export([hi/0]).\nhi() -> ok.\n"), 0o644)
	out := t.TempDir()

	appMod, modules, registered, err := buildApp([]string{erl}, out)
	if err != nil {
		t.Fatalf("buildApp: %v", err)
	}
	if appMod != "" {
		t.Errorf("appMod = %q, want empty (native modules are non-application)", appMod)
	}
	if len(registered) != 0 {
		t.Errorf("registered = %v, want none", registered)
	}
	if len(modules) != 1 || modules[0] != "greeting" {
		t.Fatalf("modules = %v, want [greeting]", modules)
	}
	if b, err := os.ReadFile(filepath.Join(out, "greeting.erl")); err != nil || !strings.Contains(string(b), "-module(greeting)") {
		t.Fatalf("native .erl not copied through: %v", err)
	}
}

func TestBuildAppNativeErlCollision(t *testing.T) {
	src := t.TempDir()
	// A Go module named "m" and a native m.erl collide on outPath(out, "m").
	goSrc := filepath.Join(src, "main.go")
	os.WriteFile(goSrc, []byte("package m\nfunc Serve() {}\n"), 0o644)
	erl := filepath.Join(src, "m.erl")
	os.WriteFile(erl, []byte("-module(m).\n"), 0o644)
	out := t.TempDir()

	_, _, _, err := buildApp([]string{goSrc, erl}, out)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite-refusal on collision, got %v", err)
	}
}

func TestBuildAppNativeErlInvalidName(t *testing.T) {
	src := t.TempDir()
	erl := filepath.Join(src, "bad..name.erl") // basename contains ".."
	os.WriteFile(erl, []byte("-module(x).\n"), 0o644)
	_, _, _, err := buildApp([]string{erl}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "invalid native module") {
		t.Fatalf("expected invalid-name rejection, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/cli/ -run 'TestBuildAppNativeErl|TestBuildAppAcceptsNativeErl' -v`
Expected: FAIL — the `.erl` is fed to `transpile.Module`, which errors (not valid Go), so `TestBuildAppAcceptsNativeErl` fails on the transpile error and the collision test fails differently than asserted.

- [ ] **Step 3: Add the `.erl` branch to `buildApp`**

In `internal/pkg/cli/cli.go`, at the top of the `for _, path := range paths {` loop in `buildApp`, before `src, err := os.ReadFile(path)`:

```go
		if strings.HasSuffix(path, ".erl") {
			// Native escape hatch: a hand-written Erlang module. erlc guarantees
			// -module(x) matches x.erl, so the basename is the module name.
			// Validate it (the only injection surface — it is spliced into paths
			// and systools -eval terms) and copy the source through unchanged;
			// wm release erlc's it like any transpiled module.
			mod := strings.TrimSuffix(filepath.Base(path), ".erl")
			if !validAppName(mod) {
				return appMod, modules, registered, fmt.Errorf("invalid native module name %q (from %s)", mod, path)
			}
			dst := outPath(out, mod)
			if _, err := os.Stat(dst); err == nil {
				return appMod, modules, registered, fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", dst)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return appMod, modules, registered, err
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return appMod, modules, registered, err
			}
			modules = append(modules, mod)
			continue
		}
```

Verify `strings` and `path/filepath` are already imported in `cli.go` (they are — `filepath` is used by `outPath`, `strings` by `parseVsnFlag`). No import change needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/cli/ -run 'TestBuildApp' -v`
Expected: PASS (all three new tests plus the existing `TestBuildOutFlagAndCollision`).

- [ ] **Step 5: Run the full cli package + build to confirm no regression**

Run: `go build -o bin/wm ./cmd/wm && go test ./internal/pkg/cli/`
Expected: `ok  go.muehmer.eu/wintermute/internal/pkg/cli`

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): accept native .erl inputs in buildApp"
```

---

### Task 2: Native fixture + usage strings + README note

Add the one native fixture file the integration tests consume (reusing the persistent Go app/sup/client), and document `.erl` inputs.

**Files:**
- Create: `testdata/native/echoserver.erl`
- Modify: `internal/pkg/cli/cli.go` (usage strings for `build`/`release` — locate the `usage:` strings)
- Modify: `internal/pkg/cli/release.go:57` (release usage string)
- Modify: `README.md`
- Test: `internal/pkg/cli/cli_test.go` (usage assertion)

**Interfaces:**
- Produces: `testdata/native/echoserver.erl` — module `echoserver`, exports `init/1, handle_call/3, start/0`, registers `{global, echo}`. Drop-in for the persistent Go supervisor's child spec `{echoserver, start, []}`. Uses a `-record` and a guard.

- [ ] **Step 1: Write the native fixture**

Create `testdata/native/echoserver.erl`:

```erlang
-module(echoserver).
-behaviour(gen_server).
-export([init/1, handle_call/3, start/0]).

%% A record for state and a guard on the callback: constructs the Go->Erlang
%% transpiler cannot express, demonstrating the whole-module native escape hatch.
%% Drop-in for the persistent fixture's Go supervisor child spec {echoserver,
%% start, []}; registers {global, echo} so a transpiled-Go client reaches it via
%% otp.CallGlobal("echo", ...). The guard accepts binary|list so it matches
%% whatever the transpiler emits for a Go string argument.
-record(state, {count = 0}).

init(_) -> {ok, #state{}}.

handle_call(Req, _From, #state{count = C} = S) when is_binary(Req); is_list(Req) ->
    {reply, Req, S#state{count = C + 1}}.

start() -> gen_server:start_link({global, echo}, ?MODULE, [], []).
```

- [ ] **Step 2: Write the failing usage test**

Append to `internal/pkg/cli/cli_test.go`:

```go
func TestUsageMentionsErlInputs(t *testing.T) {
	var buf bytes.Buffer
	// release with no source paths prints its usage to the error.
	err := Run(context.Background(), []string{"release"}, strings.NewReader(""), &buf, &buf)
	if err == nil || !strings.Contains(err.Error(), ".erl") {
		t.Fatalf("release usage should mention .erl inputs, got err=%v", err)
	}
}
```

Confirm `bytes` and `context` are imported in the test file (add if missing).

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestUsageMentionsErlInputs -v`
Expected: FAIL — current usage string does not contain `.erl`.

- [ ] **Step 4: Update the usage strings**

In `internal/pkg/cli/release.go:57`, change the usage message to mention `.erl`:

```go
		return fmt.Errorf("usage: wm release <path>... [--name N] [--out DIR] [--version X] [--vsn V] [--tar] [--self-contained]\n  <path> may be a .go source or a hand-written .erl module")
```

In `internal/pkg/cli/cli.go`, the `wm build` usage (in `buildCmd`, currently `usage: wm build <path>... [--out DIR] [--vsn X]`): append the same clarifying line:

```go
		return fmt.Errorf("usage: wm build <path>... [--out DIR] [--vsn X]\n  <path> may be a .go source or a hand-written .erl module")
```

- [ ] **Step 5: Add the README note**

In `README.md`, add a short subsection (place it after the build/release usage section):

```markdown
### Native Erlang modules (escape hatch)

Wintermute is Go-first but Erlang-capable. When the Go subset cannot express
what OTP needs (records, macros, complex guards, binary pattern matching, list
comprehensions), hand-write a `.erl` module and pass it to `wm build`/`wm release`
alongside your `.go` sources:

    wm release app.go sup.go server.erl --out dist

The `.erl` file bypasses the transpiler, is compiled natively with `erlc`, and is
packaged into the release. Transpiled Go and native modules interoperate through
the normal OTP mechanisms — a native `gen_server` registered as `{global, Name}`
is reachable from Go via `otp.CallGlobal("Name", ...)`. Native modules are
libraries/servers; the application and supervisor modules stay Go.
```

- [ ] **Step 6: Run the usage test + build**

Run: `go test ./internal/pkg/cli/ -run TestUsage -v && go build -o bin/wm ./cmd/wm`
Expected: PASS; clean build.

- [ ] **Step 7: Commit**

```bash
git add testdata/native/echoserver.erl internal/pkg/cli/cli.go internal/pkg/cli/release.go internal/pkg/cli/cli_test.go README.md
git commit -s -m "feat(cli): document native .erl inputs, add native fixture"
```

---

### Task 3: CLI integration e2e — real `wm release` with a native module (layer 2)

The primary new-path test: drive the real `wm release --self-contained` with the native server swapped in for the Go server, boot under a scrubbed environment, and assert a control node gets `hello` from `{global, echo}`. This is the only test that exercises `buildApp`'s `.erl` branch through the actual CLI pipeline. Modeled on the proven `TestSelfContainedTargetSystemEndToEnd`.

**Files:**
- Create: `internal/pkg/cli/native_integration_test.go`

**Interfaces:**
- Consumes: `Run(ctx, args, stdin, stdout, stderr) error` (package `cli`), `release.Untar` (`internal/pkg/release`), `mustErts(t, root)` and `erlang.NewLayout`/`erlang.DefaultVersion` (already used in `selfcontained_integration_test.go`, same package — reuse directly).
- Reuses fixtures: `testdata/persistent/go/echoapp/main.go`, `.../echosup/main.go` (Go), `testdata/native/echoserver.erl` (native). The Go `echosup` references `echoserver.Start` → transpiles to `{echoserver, start, []}`, satisfied by the native module.

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/cli/native_integration_test.go`:

```go
//go:build integration

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/erlang"
	"go.muehmer.eu/wintermute/internal/pkg/release"
)

// TestReleaseWithNativeErlModule drives the real `wm release --self-contained`
// with a MIXED input set: the app and supervisor are transpiled Go, the echo
// server is a hand-written .erl module (record + guard). It boots the target
// system under a fully scrubbed environment (no system Erlang) and drives a
// scrubbed control node to resolve {global, echo} and call it — proving a native
// .erl input survives buildApp routing, erlc, release + target-system assembly,
// and boots interoperably with transpiled Go.
func TestReleaseWithNativeErlModule(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skipf("local Erlang %s not installed", erlang.DefaultVersion)
	}
	out := t.TempDir()
	vsn := "0.2.7"
	srcs := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
		"../../../testdata/native/echoserver.erl", // native, swapped in for the Go server
	}
	ctx := context.Background()

	args := append([]string{"release", "--out", out, "--vsn", vsn, "--self-contained"}, srcs...)
	if err := Run(ctx, args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("wm release --self-contained: %v", err)
	}

	unpack := t.TempDir()
	f, err := os.Open(filepath.Join(out, "echoapp-"+vsn+".tar.gz"))
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if err := release.Untar(f, unpack); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	target := filepath.Join(unpack, "echoapp-"+vsn)

	// Native module's .beam must be in the bundle.
	if _, err := os.Stat(filepath.Join(target, "lib", "echoapp-"+vsn, "ebin", "echoserver.beam")); err != nil {
		t.Fatalf("native echoserver.beam not packaged: %v", err)
	}

	node := fmt.Sprintf("wmnat_%d@127.0.0.1", os.Getpid())
	if err := os.WriteFile(filepath.Join(target, "releases", vsn, "vm.args"), []byte("-name "+node+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scrub := []string{"PATH=/usr/bin:/bin", "HOME=" + target}
	erlBin := filepath.Join(target, "erts-"+mustErts(t, target), "bin", "erl")

	start := exec.CommandContext(ctx, filepath.Join(target, "bin", "start"))
	start.Env = scrub
	if o, err := start.CombinedOutput(); err != nil {
		t.Fatalf("bin/start: %v\n%s", err, o)
	}
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		_ = stop.Run()
	})

	callEval := fmt.Sprintf(
		`Wait = fun Loop(0) -> erlang:error(timeout); Loop(N) -> net_adm:ping('%s'), global:sync(), `+
			`case global:whereis_name(echo) of undefined -> timer:sleep(100), Loop(N-1); _ -> ok end end, `+
			`Wait(30), R = gen_server:call({global,echo}, <<"hello">>), io:format("~s", [R]), init:stop().`, node)
	ctrl := exec.CommandContext(ctx, erlBin,
		"-boot", filepath.Join(target, "releases", vsn, "start_clean"),
		"-name", fmt.Sprintf("wmnatctrl_%d@127.0.0.1", os.Getpid()), "-noshell", "-eval", callEval)
	ctrl.Env = scrub
	got, err := ctrl.CombinedOutput()
	if err != nil {
		t.Fatalf("scrubbed control node: %v\n%s", err, got)
	}
	if strings.TrimSpace(string(got)) != "hello" {
		t.Fatalf("native-module reply = %q, want hello", got)
	}
}
```

- [ ] **Step 2: Ensure Erlang is installed, then run the test**

Run:
```bash
./bin/wm erlang install   # no-op if OTP 29.0.3 already at ~/.local/erlang/29.0.3
go test -tags integration ./internal/pkg/cli/ -run TestReleaseWithNativeErlModule -v
```
Expected: PASS (`hello`). If it fails on the guard (transpiler emits a Go string as neither binary nor list), widen the fixture guard in `testdata/native/echoserver.erl` accordingly and re-run — but binary|list covers both current emissions.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/cli/native_integration_test.go
git commit -s -m "test(cli): integration e2e for native .erl module in wm release"
```

---

### Task 4: Ladder rung — native module in a supervised release (layer 3)

Belt-and-suspenders: feed the native server into the ladder's `buildEchoRelease` alongside the transpiled Go app/sup, boot the release, and let the transpiled-Go `echoclient` call it. Re-confirms OTP-behaviour interchangeability inside a supervised release; it does not touch `buildApp`, so it complements Task 3.

**Files:**
- Create: `internal/pkg/ladder/ladder_native_integration_test.go`

**Interfaces:**
- Consumes: `runRelease(t, idx, vsn, erls []string, appFile string) string`, `erlPersistClient` const, `transpile.Module`, `transpile.AppResource`, `erlang.NewLayout`/`Installed` — all in package `ladder` (same package, `//go:build integration`).
- Reuses fixtures: `testdata/persistent/go/{echoapp,echosup}/main.go` (Go), `testdata/native/echoserver.erl` (native), `testdata/persistent/erlang/echoclient.erl` (client, via `runRelease`'s control node).

- [ ] **Step 1: Write the failing test**

Create `internal/pkg/ladder/ladder_native_integration_test.go`:

```go
//go:build integration

package ladder

import (
	"os"
	"path/filepath"
	"testing"

	"go.muehmer.eu/wintermute/internal/pkg/transpile"
)

// TestRungVIII_NativeErlModule builds a supervised release whose echo server is a
// hand-written .erl module (record + guard) while the app and supervisor are
// transpiled Go, boots it, and has the transpiled-Go echoclient call {global,
// echo}. Proves a native module drops into a Wintermute release and interoperates
// with transpiled Go at the supervised-release level.
func TestRungVIII_NativeErlModule(t *testing.T) {
	dir := t.TempDir()
	erls, appFile := mixedNativeApp(t, dir)
	got := runRelease(t, 8, "0.2.7", erls, appFile)
	if got != "hello" {
		t.Fatalf("rung VIII = %q, want hello", got)
	}
}

// mixedNativeApp transpiles the persistent Go app + supervisor into <dir>, copies
// the native echoserver.erl in, and generates echoapp.app listing all three
// modules. Mirrors transpilePersistentApp but swaps the Go server for the native
// module. Returns the .erl paths and the .app path.
func mixedNativeApp(t *testing.T, dir string) ([]string, string) {
	t.Helper()
	goFiles := []string{
		"../../../testdata/persistent/go/echoapp/main.go",
		"../../../testdata/persistent/go/echosup/main.go",
	}
	modules := []string{}
	var registered []string
	var appMod string
	var erls []string
	for _, gf := range goFiles {
		src, err := os.ReadFile(gf)
		if err != nil {
			t.Fatal(err)
		}
		r, err := transpile.Module(string(src))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, r.Module+".erl")
		if err := os.WriteFile(p, []byte(r.Erl), 0o644); err != nil {
			t.Fatal(err)
		}
		erls = append(erls, p)
		modules = append(modules, r.Module)
		registered = append(registered, r.Registered...)
		if r.Behaviour == "application" {
			appMod = r.Module
		}
	}
	// Native server, copied through as-is.
	nativeSrc, err := os.ReadFile("../../../testdata/native/echoserver.erl")
	if err != nil {
		t.Fatal(err)
	}
	nativeDst := filepath.Join(dir, "echoserver.erl")
	if err := os.WriteFile(nativeDst, nativeSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	erls = append(erls, nativeDst)
	modules = append(modules, "echoserver")

	appFile := filepath.Join(dir, appMod+".app")
	body := transpile.AppResource(appMod, "0.2.7", modules, registered)
	if err := os.WriteFile(appFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return erls, appFile
}
```

- [ ] **Step 2: Run the rung**

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRungVIII_NativeErlModule -v`
Expected: PASS (`hello`).

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/ladder/ladder_native_integration_test.go
git commit -s -m "test(ladder): rung VIII native .erl module in supervised release"
```

---

### Task 5: Full verification gate

Confirm the whole suite (unit + all integration rungs) is green and the security tools show no new unaccepted findings, then bump VERSION.

**Files:**
- Modify: `VERSION`

- [ ] **Step 1: Bump VERSION**

Set `VERSION` file contents to `0.2.7`.

- [ ] **Step 2: Unit suite + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: all packages `ok`.

- [ ] **Step 3: Full integration ladder + CLI integration**

Run:
```bash
go test -tags integration ./internal/pkg/ladder/    # existing 23 rungs + new rung VIII
go test -tags integration ./internal/pkg/cli/        # 0.2.5 e2e, rung VII, + new native e2e
```
Expected: all PASS.

- [ ] **Step 4: Security gate**

Run:
```bash
govulncheck ./...
gitleaks detect
gosec ./...
```
Expected: `govulncheck`/`gitleaks` clean. `gosec` — confirm no NEW unaccepted HIGH/CRITICAL category beyond the accepted `G204`/`G304`/`G306`/`G104` set documented in the 0.2.6 handover (this change adds only a file copy + one `erlc` invocation on the user's own source). Address or `#nosec`-annotate with rationale any genuinely-new finding.

- [ ] **Step 5: Commit**

```bash
git add VERSION
git commit -s -m "chore: bump VERSION to 0.2.7"
```

---

## Self-Review

**Spec coverage:**
- `.erl` accepted as build/release input → Task 1. ✓
- Bypass transpiler, basename = module, `validAppName` guard, copy-through, exists-guard, no `appMod`/`registered` → Task 1. ✓
- Interop via existing OTP (no new marker) → Tasks 3 & 4 exercise `otp.CallGlobal`/`{global,echo}`; no marker added. ✓
- Security (basename injection surface, `validAppName`) → Task 1 test `TestBuildAppNativeErlInvalidName` + Task 5 gosec gate. ✓
- Layer 1 unit (`buildApp`) → Task 1. ✓
- Layer 2 CLI integration (real `wm release`, mixed input, boot, hello) → Task 3. ✓
- Layer 3 ladder rung → Task 4. ✓
- Fixture = enhanced `echoserver.erl` (record + guard), Go app/sup/client reused → Task 2 + reuse in 3/4. ✓
- Usage strings + README → Task 2. ✓
- Version 0.2.7 → Task 5. ✓
- Backlog items (native app module, `otp.Apply`, inline B) → correctly NOT implemented (deferred in spec). ✓

**Placeholder scan:** No TBD/TODO; all steps carry concrete code and commands. ✓

**Type consistency:** `buildApp` signature unchanged across tasks; `runRelease`/`buildEchoRelease`/`transpile.Module`/`transpile.AppResource`/`release.Untar`/`mustErts` signatures match the existing code read during planning; `mixedNativeApp` returns `([]string, string)` matching `runRelease`'s `erls, appFile` params. ✓
