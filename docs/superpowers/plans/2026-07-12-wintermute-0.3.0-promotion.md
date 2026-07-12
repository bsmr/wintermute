# Wintermute 0.3.0 Promotion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote the feature-complete 0.2.x line to 0.3.0 through a review + fix sweep — harden the short-lived control nodes (cookie off argv), DRY their preamble, close a validation gap, fix a test-node leak, and run a security review — with no new capability.

**Architecture:** All changes are in `internal/pkg/cli`. The core is one cohesive refactor of the four control-node commands (`stop`/`status`/`call`/`attach`): a shared `controlTarget` preamble resolver (folding in `ValidateVersion`) and a `cookieArgsFile` helper that passes the node cookie via a `0o600` `erl -args_file` instead of `-setcookie` on argv (mirroring `startCmd`'s 0.2.5 run-file). Then a test-cleanup fix, a lint nit, a `security-review` sweep, and the release gate.

**Tech Stack:** Go (stdlib only), Erlang/OTP 29.0.3 toolchain, Go `testing` (unit + `//go:build integration`).

## Global Constraints

- **Stdlib only.** No third-party Go modules. (project `CLAUDE.md`)
- **TDD, red → green.** Failing test first, then implement.
- **Promotion = no new feature.** Every change is a fix/refactor/hardening of existing code. No transpiler change, no `pkg/otp` change.
- **Build output to `bin/` only:** `go build -o bin/wm ./cmd/wm`.
- **Module path is `go.muehmer.eu/wintermute`.**
- **Cookie is RCE-grade:** it must never appear on argv (visible via `/proc`/`ps`); it lives only in a `0o600` file loaded via `erl -args_file`.
- **Review bound:** the `security-review` sweep folds only Critical/Important findings into 0.3.0; Minor/feature-shaped findings go to the 0.3.x backlog.
- **Version:** 0.3.0. Branch: `development-0.3.0-work`.
- **Commits:** `git commit -s`, conventional-commit style, English, trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Language:** replies to the user in German; all code/comments/commits in English.

---

### Task 1: `cookieArgsFile` helper

An isolated helper: write a cookie to a fresh `0o600` file loadable via `erl -args_file`, plus a cleanup to remove it. No command changes yet.

**Files:**
- Modify: `internal/pkg/cli/node.go` (add helper near `newCookie`)
- Test: `internal/pkg/cli/cli_test.go` (append)

**Interfaces:**
- Consumes: nothing new (`os`, `strings` already imported in `node.go`).
- Produces: `func cookieArgsFile(cookie string) (path string, cleanup func(), err error)` — writes `-setcookie <cookie>\n` to a `0o600` temp file; `cleanup` removes it.

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/cli/cli_test.go`:

```go
func TestCookieArgsFileWritesOwnerOnly(t *testing.T) {
	path, cleanup, err := cookieArgsFile("c0ffee")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
	body, _ := os.ReadFile(path)
	if strings.TrimSpace(string(body)) != "-setcookie c0ffee" {
		t.Errorf("body = %q, want -setcookie c0ffee", body)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove %s", path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestCookieArgsFile -v`
Expected: FAIL — `undefined: cookieArgsFile`.

- [ ] **Step 3: Implement the helper**

In `internal/pkg/cli/node.go`, after `newCookie` (around line 116):

```go
// cookieArgsFile writes cookie to a fresh 0o600 file loadable via `erl
// -args_file`, so a short-lived control node authenticates WITHOUT exposing the
// (RCE-grade) cookie on argv (visible via /proc or `ps`). The caller must defer
// the returned cleanup to remove the file. Mirrors startCmd's long-lived run-file.
func cookieArgsFile(cookie string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "wm-cookie-*.args")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	cleanup = func() { _ = os.Remove(name) }
	// CreateTemp already makes the file 0o600, but Chmod unconditionally to be
	// explicit about the owner-only guarantee (mirrors startCmd's run-file).
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if _, err := f.WriteString("-setcookie " + cookie + "\n"); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/cli/ -run TestCookieArgsFile -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/node.go internal/pkg/cli/cli_test.go
git commit -s -m "feat(cli): add cookieArgsFile helper (cookie via 0o600 args_file)"
```

---

### Task 2: Rewire the control-node commands (cookie off argv + shared preamble + ValidateVersion)

The core refactor. Add a `controlTarget` preamble resolver, rewire all four commands to load the cookie via `cookieArgsFile` (off argv), and fold `ValidateVersion` into the shared path. Existing assemble-tests that assert `-setcookie` on argv must be updated.

**Files:**
- Modify: `internal/pkg/cli/cli.go` (add `controlTarget`; rewrite `stopCmd`/`statusCmd`/`callCmd`/`attachCmd`)
- Test: `internal/pkg/cli/cli_test.go` (update 3 existing tests; add cookie-off-argv + ValidateVersion tests)

**Interfaces:**
- Consumes: `cookieArgsFile` (Task 1); `resolveApp`, `parseVersionFlag`, `readState`, `removeState`, `ctrlNode`, `validAtom`, `runErl`, `captureErl`, `attachErl`, `erlang.NewLayout`, `erlang.ValidateVersion`, `NodeState` (existing).
- Produces: `func controlTarget(args []string) (app string, st NodeState, l erlang.Layout, err error)` — the shared preamble (resolveApp → parseVersionFlag → ValidateVersion → readState → NewLayout).

- [ ] **Step 1: Write the failing tests (cookie off argv + ValidateVersion)**

Append to `internal/pkg/cli/cli_test.go`:

```go
func TestStopCookieOffArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})
	var joined string
	runErl = func(_ context.Context, _, name string, a ...string) error {
		joined = name + " " + strings.Join(a, " ")
		return nil
	}
	defer func() { runErl = execRunner }()
	if err := Run(context.Background(), []string{"stop", "echoapp"},
		strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joined, "-setcookie") || strings.Contains(joined, "c0ffee") {
		t.Fatalf("cookie leaked on argv: %s", joined)
	}
	if !strings.Contains(joined, "-args_file") {
		t.Fatalf("stop should pass -args_file: %s", joined)
	}
}

func TestStatusCookieOffArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})
	var joined string
	orig := captureErl
	captureErl = func(_ context.Context, _, _ string, a ...string) ([]byte, error) {
		joined = strings.Join(a, " ")
		return []byte("pong\n"), nil
	}
	defer func() { captureErl = orig }()
	if err := Run(context.Background(), []string{"status", "echoapp"},
		strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joined, "-setcookie") || strings.Contains(joined, "c0ffee") {
		t.Fatalf("cookie leaked on argv: %s", joined)
	}
	if !strings.Contains(joined, "-args_file") {
		t.Fatalf("status should pass -args_file: %s", joined)
	}
}

func TestStopRejectsInvalidVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})
	err := Run(context.Background(), []string{"stop", "echoapp", "--version", "1.2"},
		strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("stop should reject bad version, got %v", err)
	}
}

func TestStatusRejectsInvalidVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeState("echoapp", NodeState{Node: "echoapp@127.0.0.1", Cookie: "c0ffee", CodePath: "bin"})
	err := Run(context.Background(), []string{"status", "echoapp", "--version", "1.2"},
		strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("status should reject bad version, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/pkg/cli/ -run 'CookieOffArgv|RejectsInvalidVersion' -v`
Expected: FAIL — cookie is still on argv (`-setcookie c0ffee` present), and `stop`/`status` do not yet validate the version (`--version 1.2` is a valid *flag* but an invalid N.N.N version that the command currently ignores).

- [ ] **Step 3: Add the `controlTarget` preamble helper**

In `internal/pkg/cli/cli.go`, immediately before `stopCmd` (around line 480):

```go
// controlTarget resolves the target of a control-node command: the app name, its
// persisted node state, and the erlang layout. It validates --version in one
// shared place (previously stop/status/attach skipped ValidateVersion). Used by
// stop/status/attach; call does its own arg parsing but reuses cookieArgsFile.
func controlTarget(args []string) (app string, st NodeState, l erlang.Layout, err error) {
	app, rest, err := resolveApp(args)
	if err != nil {
		return "", NodeState{}, erlang.Layout{}, err
	}
	version, _, err := parseVersionFlag(rest)
	if err != nil {
		return "", NodeState{}, erlang.Layout{}, err
	}
	if err = erlang.ValidateVersion(version); err != nil {
		return "", NodeState{}, erlang.Layout{}, err
	}
	st, err = readState(app)
	if err != nil {
		return "", NodeState{}, erlang.Layout{}, err
	}
	home, _ := os.UserHomeDir()
	return app, st, erlang.NewLayout(home, version), nil
}
```

- [ ] **Step 4: Rewrite `stopCmd`**

Replace the body of `stopCmd` with:

```go
func stopCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, st, l, err := controlTarget(args)
	if err != nil {
		return err
	}
	cf, cleanup, err := cookieArgsFile(st.Cookie)
	if err != nil {
		return err
	}
	defer cleanup()
	eval := fmt.Sprintf("rpc:call('%s', init, stop, []), init:stop().", st.Node)
	if err := runErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-args_file", cf, "-noshell", "-eval", eval); err != nil {
		return err
	}
	if err := removeState(app); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "stopped %s\n", app)
	return nil
}
```

- [ ] **Step 5: Rewrite `statusCmd`**

Replace the body of `statusCmd` with:

```go
func statusCmd(ctx context.Context, args []string, stdout io.Writer) error {
	app, st, l, err := controlTarget(args)
	if err != nil {
		return err
	}
	cf, cleanup, err := cookieArgsFile(st.Cookie)
	if err != nil {
		return err
	}
	defer cleanup()
	eval := fmt.Sprintf(
		"io:format(\"~p~n\", [net_adm:ping('%s')]), "+
			"io:format(\"~p~n\", [rpc:call('%s', application, which_applications, [])]), "+
			"init:stop().", st.Node, st.Node)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-args_file", cf, "-noshell", "-eval", eval)
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	fmt.Fprintf(stdout, "%s (%s):\n%s", app, st.Node, out)
	return nil
}
```

- [ ] **Step 6: Rewrite `callCmd`'s tail (keep its arg parsing; add ValidateVersion + cookieArgsFile)**

`callCmd` keeps its custom parsing (`--app`, positional `name`/`req`, `validAtom`, `resolveApp(nil)` fallback). Change: (a) validate the version, (b) load cookie via args_file. Replace the section from `st, err := readState(app)` to the `captureErl` call with:

```go
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
	st, err := readState(app)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, version)
	cf, cleanup, err := cookieArgsFile(st.Cookie)
	if err != nil {
		return err
	}
	defer cleanup()
	// The control node is fresh, so it must connect to the target node and
	// converge the global name registry before {global, <name>} resolves. After
	// a detached boot the registration is not instant, so poll net_adm:ping +
	// global:sync until whereis resolves (bounded) before issuing the call —
	// otherwise the cross-node gen_server:call races the registration and fails.
	eval := fmt.Sprintf(
		"Wait = fun Loop(0) -> erlang:error(global_name_timeout); "+
			"Loop(N) -> net_adm:ping('%s'), global:sync(), "+
			"case global:whereis_name(%s) of "+
			"undefined -> timer:sleep(100), Loop(N - 1); _ -> ok end end, "+
			"Wait(30), "+
			"io:format(\"~s~n\", [gen_server:call({global, %s}, <<%q>>)]), init:stop().",
		st.Node, name, name, req)
	out, err := captureErl(ctx, ".", l.Erl(), "-name", ctrlNode(),
		"-args_file", cf, "-noshell", "-eval", eval)
```

(The lines after — the `if err != nil { return fmt.Errorf("call failed...") }` and `fmt.Fprint(stdout, ...)` — are unchanged. `callCmd` already binds `version` earlier via `parseVersionFlag`.)

- [ ] **Step 7: Rewrite `attachCmd`**

Replace the body of `attachCmd` with:

```go
func attachCmd(ctx context.Context, args []string, stdout io.Writer) error {
	_, st, l, err := controlTarget(args)
	if err != nil {
		return err
	}
	cf, cleanup, err := cookieArgsFile(st.Cookie)
	if err != nil {
		return err
	}
	defer cleanup()
	// A unique-per-invocation control node avoids clashing with a prior attach.
	ctrl := "wmattach@127.0.0.1"
	return attachErl(ctx, ".", l.Erl(), "-remsh", st.Node, "-name", ctrl, "-args_file", cf)
}
```

- [ ] **Step 8: Update the 3 existing assemble-tests that assert `-setcookie` on argv**

In `internal/pkg/cli/cli_test.go`:

- `TestStopAssemblesRpcAndRemovesState`: change the `want` slice from
  `[]string{"-setcookie c0ffee", "rpc:call('echoapp@127.0.0.1', init, stop, [])"}`
  to `[]string{"-args_file", "rpc:call('echoapp@127.0.0.1', init, stop, [])"}`.
- `TestCallAppOverrideSelectsNamedNode`: replace the assertion
  ```go
  if !strings.Contains(joined, "-setcookie aaa") {
      t.Fatalf("call cmd should use echoapp's cookie (-setcookie aaa):\n%s", joined)
  }
  ```
  with
  ```go
  if strings.Contains(joined, "-setcookie") || strings.Contains(joined, "aaa") {
      t.Fatalf("cookie leaked on argv:\n%s", joined)
  }
  if !strings.Contains(joined, "-args_file") {
      t.Fatalf("call should pass -args_file:\n%s", joined)
  }
  ```
- `TestAttachAssemblesRemsh`: change the `want` slice from
  `[]string{"-remsh echoapp@127.0.0.1", "-setcookie c0ffee"}`
  to `[]string{"-remsh echoapp@127.0.0.1", "-args_file"}`.

- [ ] **Step 9: Run the full cli unit suite**

Run: `go build -o bin/wm ./cmd/wm && go test ./internal/pkg/cli/`
Expected: `ok` — the new cookie-off-argv + ValidateVersion tests pass, and the updated assemble-tests pass.

- [ ] **Step 10: Commit**

```bash
git add internal/pkg/cli/cli.go internal/pkg/cli/cli_test.go
git commit -s -m "refactor(cli): control-node cookie via args_file, shared preamble, validate version"
```

---

### Task 3: Integration-test node-leak fix + CutSuffix nit

Fix the `t.Cleanup` that leaks detached `beam.smp` nodes, and apply the one clean `strings.CutSuffix` conversion.

**Files:**
- Modify: `internal/pkg/cli/native_integration_test.go`, `internal/pkg/cli/selfcontained_integration_test.go` (cleanup)
- Modify: `internal/pkg/cli/cli.go` (`resolveApp`, line ~460)

**Interfaces:** none new.

- [ ] **Step 1: Fix the cleanup in both integration tests**

In BOTH `internal/pkg/cli/native_integration_test.go` and `internal/pkg/cli/selfcontained_integration_test.go`, the cleanup currently is:

```go
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		_ = stop.Run()
	})
```

Replace it (in each file) with a version that falls back to SIGKILL for any node still rooted at this test's unique unpack dir — `unpack` is the test's `t.TempDir()`, so the pattern is unique to this test and cannot kill unrelated nodes:

```go
	t.Cleanup(func() {
		stop := exec.Command(filepath.Join(target, "bin", "stop"))
		stop.Env = scrub
		if err := stop.Run(); err != nil {
			// Fallback: SIGKILL any beam still rooted at this test's unique dir,
			// so a failed graceful stop does not leak a node into epmd.
			_ = exec.Command("pkill", "-9", "-f", unpack).Run()
		}
	})
```

(If the local variable holding the test's `t.TempDir()` is not named `unpack` in a given file, use that file's variable for the unpacked target root. In `native_integration_test.go` and `selfcontained_integration_test.go` it is `unpack`.)

- [ ] **Step 2: Verify the integration tests still pass (and leave no leaked node)**

Run:
```bash
pkill -9 -x beam.smp 2>/dev/null; pkill -9 -x epmd 2>/dev/null; sleep 1
go test -tags integration ./internal/pkg/cli/ -run 'TestReleaseWithNativeErlModule|TestSelfContainedTargetSystemEndToEnd' -v
pgrep -xc beam.smp   # expect 0 after the run
```
Expected: both tests PASS; `pgrep -xc beam.smp` prints `0` (no leaked node).

- [ ] **Step 3: Apply the CutSuffix nit in `resolveApp`**

In `internal/pkg/cli/cli.go`, in `resolveApp` (around line 459-461), replace:

```go
		if strings.HasSuffix(e.Name(), ".json") {
			apps = append(apps, strings.TrimSuffix(e.Name(), ".json"))
		}
```

with:

```go
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			apps = append(apps, name)
		}
```

(The other `HasSuffix`+`TrimSuffix` pair in `buildApp` operates on two different values — `path` vs `filepath.Base(path)` — so it has no clean `CutSuffix` form; leave it. `absEbin` escaping is deliberately NOT changed, per the spec.)

- [ ] **Step 4: Run the cli unit suite + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./internal/pkg/cli/`
Expected: `ok` (unchanged behaviour; `resolveApp` still lists apps).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/native_integration_test.go internal/pkg/cli/selfcontained_integration_test.go internal/pkg/cli/cli.go
git commit -s -m "test(cli): SIGKILL fallback for leaked integration nodes; CutSuffix nit"
```

---

### Task 4: security-review sweep

Run the `security-review` skill over the 0.2.x surface and triage findings against the bounding rule. This task has no pre-written code — findings are unknown until the review runs.

**Files:** determined by findings (Critical/Important only).

- [ ] **Step 1: Run the security review**

Invoke the `security-review` skill (or `/security-review`) scoped to the 0.2.x surface: the CLI commands (`internal/pkg/cli/*.go`), the release/archive code (`internal/pkg/release/*.go`), cookie handling, path/atom validation (`validAppName`/`validVsn`/`validNodeName`/`validAtom`), and the `systools`/`erlc` `-eval` interpolations in `release.go`.

- [ ] **Step 2: Triage findings**

For each finding: **Critical/Important → fold into 0.3.0** (this task); **Minor or feature-shaped → record in the 0.3.x backlog** (HANDOVER), do not fix here. Record the triage in the progress ledger.

- [ ] **Step 3: Fix folded findings (TDD each)**

For each Critical/Important finding: write a failing test that demonstrates the issue, fix it minimally, confirm green. (Concrete code depends on the finding.)

- [ ] **Step 4: Run the cli suite + build**

Run: `go build -o bin/wm ./cmd/wm && go test ./internal/pkg/cli/`
Expected: `ok`.

- [ ] **Step 5: Commit (only if findings were folded)**

```bash
git add -A
git commit -s -m "fix(cli): fold in Critical/Important security-review findings"
```

If the review surfaces nothing foldable, record "security-review: no Critical/Important findings" in the ledger and skip the commit.

---

### Task 5: Verification gate + VERSION bump

Full green gate on the branch before the merge/release ceremony.

**Files:**
- Modify: `VERSION`

- [ ] **Step 1: Bump VERSION**

Set `VERSION` file contents to exactly `0.3.0` (currently `0.2.7`).

- [ ] **Step 2: Clear any leftover test nodes, then run the full suite**

Run:
```bash
pkill -9 -x beam.smp 2>/dev/null; pkill -9 -x epmd 2>/dev/null; sleep 1
go build -o bin/wm ./cmd/wm && go test ./...
go test -tags integration ./internal/pkg/ladder/
go test -tags integration ./internal/pkg/cli/
pgrep -xc beam.smp   # expect 0
```
Expected: all packages `ok`; ladder 24 rungs green; cli integration (0.2.5 e2e + rung VII + native e2e) green; no leaked node.

- [ ] **Step 3: Security tools**

Run:
```bash
govulncheck ./...
gitleaks detect --no-banner
gosec ./...
```
Expected: `govulncheck`/`gitleaks` clean. `gosec`: confirm no NEW unaccepted HIGH/CRITICAL beyond the accepted `G204`/`G304`/`G306`/`G703` classes (this change removes a `-setcookie` argv exposure and adds a `0o600` temp file — expect the HIGH count to stay at the accepted G703 set or drop).

- [ ] **Step 4: Commit**

```bash
git add VERSION
git commit -s -m "chore: bump VERSION to 0.3.0"
```

---

## Self-Review

**Spec coverage:**
- Control-node preamble refactor + `cookieArgsFile` (cookie off argv) → Tasks 1–2. ✓
- DRY preamble (`controlTarget`) → Task 2. ✓
- stop/status (and call/attach) missing `ValidateVersion` → Task 2 (folded into `controlTarget`; call gets it inline). ✓
- Integration-test `beam.smp` leak → Task 3. ✓
- `CutSuffix` nit → Task 3; `absEbin` deliberately dropped → noted in Task 3 + spec backlog. ✓
- security-review sweep, Critical/Important only → Task 4. ✓
- Verification gate, gosec class check, VERSION → Task 5. ✓
- Copilot gate + merge + tag `v0.3.0` + push → handled by the finishing-a-development-branch flow after Task 5 (release ceremony, not a plan task). ✓
- No transpiler / `pkg/otp` change → honored (all changes in `cli`/tests). ✓

**Placeholder scan:** Task 4 has no pre-written fix code by necessity (findings unknown) — this is a review task, explicitly bounded, with the fix procedure (TDD each) stated. All other steps carry concrete code/commands. No TBD/TODO.

**Type consistency:** `controlTarget` returns `(app string, st NodeState, l erlang.Layout, err error)` — matches `erlang.NewLayout`'s `erlang.Layout` return and `NodeState` (node.go). `cookieArgsFile(cookie string) (string, func(), error)` used consistently in Tasks 1–2. `l.Erl()`, `runErl`/`captureErl`/`attachErl`, `ctrlNode()`, `removeState` match existing signatures read during planning.
