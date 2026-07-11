# Wintermute 0.2.0 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clear the entire 0.1.0 review backlog (16 items) so the transpiler never emits silently-wrong Erlang, the toolchain verifies its downloads and rejects hostile input, and the accumulated code-quality nits are consolidated.

**Architecture:** Pure hardening of the existing `internal/pkg/transpile`, `internal/pkg/cli`, `internal/pkg/erlang`, and `pkg/otp` packages. No new subsystems, no new commands. Each backlog item is one independently-testable red→green TDD cycle. All tests are white-box (`package transpile` / `cli` / `erlang` / `otp`), matching the existing suite.

**Tech Stack:** Go stdlib only (`go/ast`, `go/parser`, `go/token`, `regexp`, `crypto/sha256`, `encoding/hex`, `net/http`, `net/http/httptest`, `os/exec`, `testing`). No third-party modules.

## Global Constraints

- **Module path:** `go.muehmer.eu/wintermute` (not the GitHub path).
- **Stdlib only.** No third-party modules. Drives B2 (pinned SHA-256, not sigstore).
- **Never silent-wrong.** Reject out-of-subset input with an error; never emit plausible-but-wrong Erlang. Drives A1, A2.
- **main() → run() pattern** unchanged; all logic stays in `internal/pkg/`.
- **Build output to `bin/` only:** `go build -o bin/wm ./cmd/wm`.
- **VERSION = `0.2.0`** (already bumped and committed).
- **Field-casing → reject** (locked). **Tarball verify → pinned SHA-256** (locked).
- **Commit style:** conventional commits; sign off (`git commit -s`); trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Suite stays green:** `go test ./...` after every task. The integration ladder (`go test -tags integration ./internal/pkg/ladder/`) must still pass on the provisioned OTP 29.0.3 before merge.

---

## Task 1 (A1): Atom-collision detection

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (function loop in `File`, ~59-84)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File(src string) (string, error)`.
- Produces: no signature change; `File` now errors when two Go function names collide after lowercasing.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_AtomCollisionErrors(t *testing.T) {
	src := `package m
func Foo() {}
func foo() {}
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for Foo/foo collapsing to the same Erlang atom, got nil")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Fatalf("error should name the colliding atom, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_AtomCollisionErrors -v`
Expected: FAIL — currently both emit `foo()` (duplicate clause), no error.

- [ ] **Step 3: Write minimal implementation**

In `File`, inside the `for _, d := range f.Decls` function loop, track lowercased names. Right after `name := strings.ToLower(fn.Name.Name)` (currently line 67):

```go
		name := strings.ToLower(fn.Name.Name)
		if prev, ok := seen[name]; ok {
			return "", fmt.Errorf("functions %s and %s both map to Erlang atom %s (duplicate clause); rename one",
				prev, fn.Name.Name, name)
		}
		seen[name] = fn.Name.Name
```

Declare `seen := map[string]string{}` just before the loop (next to `var exports []string`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (new test green, existing tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): detect post-lowercase atom collisions (A1)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2 (A2): Reject lowercase-leading struct fields

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (struct-collection loop in `File`, ~46-53)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File`.
- Produces: `File` errors when a struct field name is not uppercase-leading (would be an invalid Erlang variable).

- [ ] **Step 1: Write the failing test**

```go
func TestFile_LowercaseFieldErrors(t *testing.T) {
	src := `package m
type Msg struct { text string }
func Serve() { m := otp.Receive().(Msg); otp.Print(m.text) }
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for lowercase-leading struct field (invalid Erlang variable), got nil")
	}
	if !strings.Contains(err.Error(), "text") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}
```

(Add `import "go.muehmer.eu/wintermute/pkg/otp"` line inside the src string if the parser needs it — parser does not resolve imports, so the bare `otp.` selector parses fine; no import line required.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_LowercaseFieldErrors -v`
Expected: FAIL — field collected verbatim, emitted as invalid Erlang `text`, no error.

- [ ] **Step 3: Write minimal implementation**

In the struct-collection loop, where fields are appended (currently `fields = append(fields, n.Name)`), validate first:

```go
			for _, n := range fld.Names {
				if !token.IsExported(n.Name) {
					return "", fmt.Errorf("struct %s field %s is lowercase-leading; Erlang variables must be uppercase",
						ts.Name.Name, n.Name)
				}
				fields = append(fields, n.Name)
			}
```

`token.IsExported` is already available (`go/token` is imported).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS. Existing fixtures use `From`/`Text`/`Ok`— unaffected.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): reject lowercase-leading struct fields (A2)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3 (A3): Support bare `*ast.Ident` in `emitExpr`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitExpr`, ~200-246)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitter.emitExpr(ast.Expr) (string, error)`.
- Produces: `emitExpr` now returns a bare identifier's name (a pre-bound Erlang variable) instead of erroring.

- [ ] **Step 1: Write the failing test**

```go
func TestEmitExpr_BareIdent(t *testing.T) {
	em := &emitter{}
	got, err := em.emitExpr(&ast.Ident{Name: "From"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "From" {
		t.Fatalf("emitExpr(ident From) = %q, want %q", got, "From")
	}
}
```

Ensure `import "go/ast"` is present in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestEmitExpr_BareIdent -v`
Expected: FAIL — `emitExpr` hits `default` → "unsupported expression: *ast.Ident".

- [ ] **Step 3: Write minimal implementation**

Add a case to `emitExpr` (before `default`):

```go
	case *ast.Ident:
		// A pre-bound variable reference (e.g. From/Text bound in a receive
		// pattern). A2 guarantees such names are uppercase-leading, so they
		// are valid Erlang variables as-is.
		// ponytail: no lowercase-ident guard here; local Go var decls are not
		// in the subset yet, so a lowercase bare ident cannot arise.
		return ex.Name, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): emit bare identifier as pre-bound variable (A3)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4 (A4): `file:line:` positions in transpiler errors

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitter` struct, `File`, `emitStmt`, `emitExpr`, `emitCall`)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `File`, `emitter`.
- Produces: `emitter` gains a `fset *token.FileSet` field and an `errorf(n ast.Node, format string, a ...any) error` helper. Out-of-subset errors now carry `src.go:LINE:COL:`.

- [ ] **Step 1: Write the failing test**

```go
func TestFile_ErrorsCarryPosition(t *testing.T) {
	src := `package m
func Boot() { 1 + 2 }
`
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for unsupported binary expression, got nil")
	}
	if !strings.Contains(err.Error(), "src.go:2") {
		t.Fatalf("error should carry a src.go:line position, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_ErrorsCarryPosition -v`
Expected: FAIL — error is "unsupported expression: *ast.BinaryExpr", no position.

- [ ] **Step 3: Write minimal implementation**

Add the field and pass `fset` in:

```go
type emitter struct {
	structs map[string][]string
	fset    *token.FileSet
}
```

In `File`, change `em := &emitter{structs: structs}` to `em := &emitter{structs: structs, fset: fset}`.

Add the helper near the bottom:

```go
// errorf formats an error prefixed with n's source position (src.go:line:col).
func (em *emitter) errorf(n ast.Node, format string, a ...any) error {
	return fmt.Errorf("%s: %s", em.fset.Position(n.Pos()), fmt.Sprintf(format, a...))
}
```

Convert the node-bearing error sites in `emitStmt`, `emitExpr`, and `emitCall` to use it. Examples:
- `emitStmt` default: `return "", em.errorf(s, "unsupported statement: %T", s)`
- `emitExpr` default: `return "", em.errorf(e, "unsupported expression: %T", e)`
- `emitExpr` unsupported literal: `return "", em.errorf(ex, "unsupported literal: %s", ex.Value)`
- `emitExpr` unsupported selector / composite: use `em.errorf(ex, ...)`
- `emitCall` unsupported call sites: use `em.errorf(c, ...)`

Leave `File`-level errors (A1 collision, A2 field, func-params) as plain `fmt.Errorf` — they already name the offending symbol; positions are a follow-up if needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS. Existing error tests assert on substrings that remain present.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): thread FileSet for file:line error positions (A4)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5 (A5): Roadmap context in out-of-subset messages

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (func-params error ~65; nullary-call-with-args ~252)
- Test: `internal/pkg/transpile/transpile_test.go` (extend `TestFile_FunctionWithParamsErrors`)

**Interfaces:** no signature change; error strings updated.

- [ ] **Step 1: Update the test to assert the new message**

Extend the existing `TestFile_FunctionWithParamsErrors` to check the message:

```go
	_, err := File(src)
	if err == nil {
		t.Fatal("want error for function with parameters, got nil")
	}
	if !strings.Contains(err.Error(), "0.2.x roadmap") {
		t.Fatalf("error should point at the roadmap, got: %v", err)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/transpile/ -run TestFile_FunctionWithParamsErrors -v`
Expected: FAIL — message still says "not supported in 0.1.0".

- [ ] **Step 3: Update the messages**

Replace the stale `0.1.0` wording:

```go
		return "", fmt.Errorf("unsupported function %s: parameters are not yet supported (echo subset); see the 0.2.x roadmap", fn.Name.Name)
```

And the nullary-call-with-args message in `emitCall`:

```go
			return "", em.errorf(c, "unsupported call %s with arguments: only nullary self-calls are in the subset (see the 0.2.x roadmap)", id.Name)
```

(This uses `em.errorf` from Task 4.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "feat(transpile): add roadmap context to out-of-subset errors (A5)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6 (C4): Consolidate `isOtpCall` check and indent helper

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitBody` ~117, `emitCall` ~260-262)
- Test: existing suite (behavior-locked refactor; no new test)

**Interfaces:** no signature change.

- [ ] **Step 1: Confirm suite is green**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS (baseline before refactor).

- [ ] **Step 2: Replace the bespoke 8-space indent with `indent`**

In `emitBody`, replace the hand-built clause body:

```go
		clauseBody := indent(indent(inner)) // two 4-space levels = the receive clause body
		recv := "receive\n" + indent(pat+" ->") + "\n" + clauseBody + "\nend"
```

Verify the golden output in `TestFile_ServerReceiveLoop` and `TestFile_GoldenServer` still matches; adjust the composition so the emitted string is byte-identical to the current expectation (clause pattern indented 4, body indented 8). If the exact spacing differs, keep the literal that reproduces the existing golden strings — the test is the oracle.

- [ ] **Step 3: DRY the `otp`-selector check in `emitCall`**

`emitCall` inlines `pkg.Name != "otp"`. Extract the shared notion next to `isOtpCall`:

```go
// otpPkgIdent reports whether x is the bare package identifier `otp`.
func otpPkgIdent(x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Name == "otp"
}
```

Use it in both `isOtpCall` and `emitCall`'s selector guard.

- [ ] **Step 4: Run the full suite**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: PASS — identical behavior.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/transpile/
git commit -s -m "refactor(transpile): consolidate indent and otp-ident checks (C4)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7 (C2): Return module name from `transpile.File`

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`File` signature + returns)
- Modify: `internal/pkg/cli/cli.go` (`buildCmd`, `runCmd`; delete `moduleName`)
- Test: `internal/pkg/transpile/transpile_test.go` (update all `File(` callers), `internal/pkg/cli/cli_test.go` (unaffected — asserts on files/output)

**Interfaces:**
- Consumes: `File`.
- Produces: **`File(src string) (erl string, module string, err error)`** — module is `f.Name.Name`. `cli.moduleName` is removed; callers use the returned module.

- [ ] **Step 1: Update transpile tests to the new signature (red)**

Change every `got, err := File(...)` / `_, err := File(...)` in `transpile_test.go` to the 3-value form, e.g. `got, _, err := File(...)` and `_, _, err := File(...)`. Add one assertion for the module return:

```go
func TestFile_ReturnsModuleName(t *testing.T) {
	_, mod, err := File("package echoserver\nfunc Serve() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echoserver" {
		t.Fatalf("module = %q, want echoserver", mod)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/pkg/transpile/ -v`
Expected: FAIL — build error, `File` returns 2 values not 3.

- [ ] **Step 3: Change the `File` signature**

```go
func File(src string) (string, string, error) {
	...
	if err != nil {
		return "", "", err   // update every early return to 3 values
	}
	...
	return b.String(), f.Name.Name, nil
}
```

Update every `return "", err` / `return "", fmt.Errorf(...)` inside `File` to `return "", "", ...`.

- [ ] **Step 4: Update cli callers and delete `moduleName`**

In `buildCmd`:

```go
	erl, mod, err := transpile.File(string(src))
	if err != nil {
		return err
	}
```

In `runCmd` likewise. Delete the `moduleName` function and the now-unused `strings` import if nothing else needs it (keep it if other uses remain — check the build).

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./...`
Expected: PASS.

```bash
git add internal/pkg/transpile/ internal/pkg/cli/
git commit -s -m "refactor(transpile,cli): return module name from File, drop re-parse (C2)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8 (B4): Robust `--version` flag parser

**Files:**
- Modify: `internal/pkg/cli/cli.go` (`runCmd`, `erlangCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Produces: **`parseVersionFlag(args []string) (version string, rest []string, err error)`** — resolves `--version X` and `--version=X`, defaults to `erlang.DefaultVersion` when absent, errors on a `--version` with no value.

- [ ] **Step 1: Write the failing test**

```go
func TestParseVersionFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantVersion string
		wantRest    []string
		wantErr     bool
	}{
		{"absent", []string{"main.go"}, erlang.DefaultVersion, []string{"main.go"}, false},
		{"space form", []string{"main.go", "--version", "29.0.3"}, "29.0.3", []string{"main.go"}, false},
		{"equals form", []string{"--version=29.0.3", "main.go"}, "29.0.3", []string{"main.go"}, false},
		{"missing value", []string{"main.go", "--version"}, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, rest, err := parseVersionFlag(tt.args)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if v != tt.wantVersion {
				t.Fatalf("version = %q, want %q", v, tt.wantVersion)
			}
			if strings.Join(rest, ",") != strings.Join(tt.wantRest, ",") {
				t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}
```

Add `import "go.muehmer.eu/wintermute/internal/pkg/erlang"` to the test file if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestParseVersionFlag -v`
Expected: FAIL — `parseVersionFlag` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// parseVersionFlag pulls an optional --version flag (--version X or
// --version=X) out of args, returning the resolved version (DefaultVersion if
// absent), the remaining positional args, and an error on a malformed flag.
func parseVersionFlag(args []string) (string, []string, error) {
	version := erlang.DefaultVersion
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--version requires a value")
			}
			version = args[i+1]
			i++
		case strings.HasPrefix(a, "--version="):
			version = strings.TrimPrefix(a, "--version=")
		default:
			rest = append(rest, a)
		}
	}
	return version, rest, nil
}
```

- [ ] **Step 4: Wire it into `runCmd` and `erlangCmd`**

`runCmd`:

```go
	version, rest, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: wm run <path> [--version X]")
	}
	srcPath := rest[0]
	// ... use srcPath instead of args[0], version as before
```

`erlangCmd` install branch:

```go
	case "install":
		version, rest, err := parseVersionFlag(args[1:])
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("usage: wm erlang install [--version X]")
		}
		b := erlang.Builder{Home: home, Out: stdout, Run: execRunner}
		return b.Provision(ctx, version)
```

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS.

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): robust --version flag parser (B4)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9 (B1): Validate `--version` against `^\d+\.\d+\.\d+$`

**Files:**
- Create: `internal/pkg/erlang/version.go`
- Modify: `internal/pkg/cli/cli.go` (`runCmd`, `erlangCmd` — validate after parse, before `NewLayout`/`Provision`)
- Test: `internal/pkg/erlang/version_test.go`, `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Produces: **`erlang.ValidateVersion(v string) error`** — nil for `N.N.N`, error otherwise (blocks path traversal).

- [ ] **Step 1: Write the failing test (erlang)**

`internal/pkg/erlang/version_test.go`:

```go
package erlang

import "testing"

func TestValidateVersion(t *testing.T) {
	for _, ok := range []string{"29.0.3", "0.0.0", "27.1.10"} {
		if err := ValidateVersion(ok); err != nil {
			t.Fatalf("ValidateVersion(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"../../etc", "29.0", "29.0.3/../x", "", "v29.0.3", "29.0.3-rc1"} {
		if err := ValidateVersion(bad); err == nil {
			t.Fatalf("ValidateVersion(%q) = nil, want error", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestValidateVersion -v`
Expected: FAIL — `ValidateVersion` undefined.

- [ ] **Step 3: Write minimal implementation**

`internal/pkg/erlang/version.go`:

```go
package erlang

import (
	"fmt"
	"regexp"
)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ValidateVersion rejects any version string that is not exactly N.N.N. This
// blocks path traversal into filepath.Join(... version ...) via NewLayout.
func ValidateVersion(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("invalid version %q: must be N.N.N", v)
	}
	return nil
}
```

- [ ] **Step 4: Enforce in cli + add a cli-level test**

In `runCmd` and `erlangCmd` (both branches that resolve a version), immediately after obtaining `version`:

```go
	if err := erlang.ValidateVersion(version); err != nil {
		return err
	}
```

Add to `cli_test.go`:

```go
func TestRunRejectsTraversalVersion(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Main() {}\n"), 0o644)
	t.Setenv("HOME", t.TempDir())
	err := Run(context.Background(), []string{"run", src, "--version", "../../etc"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("want error for traversal --version, got nil")
	}
}
```

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./...`
Expected: PASS.

```bash
git add internal/pkg/erlang/ internal/pkg/cli/
git commit -s -m "feat(erlang,cli): validate --version to block path traversal (B1)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10 (C1): `--out` flag, collision guard, tempdir tests

**Files:**
- Modify: `internal/pkg/cli/cli.go` (`buildCmd`, `runCmd`)
- Test: `internal/pkg/cli/cli_test.go`

**Interfaces:**
- Produces: `buildCmd`/`runCmd` accept `--out DIR` (default `bin`); refuse to overwrite an existing output file unless the dir was chosen by the caller. Output-path resolution centralized in a helper `outPath(dir, mod string) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestBuildOutFlagAndCollision(t *testing.T) {
	os.Chdir(t.TempDir()) // isolate: no bin/ left in the package dir
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package m\nfunc Serve() {}\n"), 0o644)
	out := t.TempDir()

	// --out writes into the chosen dir
	if err := Run(context.Background(), []string{"build", src, "--out", out}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "m.erl")); err != nil {
		t.Fatalf("expected m.erl in --out dir: %v", err)
	}
	// second build to the same dir collides
	if err := Run(context.Background(), []string{"build", src, "--out", out}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("want collision error on second build to same out, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestBuildOutFlagAndCollision -v`
Expected: FAIL — no `--out`, no collision guard.

- [ ] **Step 3: Write minimal implementation**

Parse `--out` alongside the source arg (reuse the positional/flag split). Minimal helper:

```go
// parseOutFlag pulls an optional --out DIR out of args (default "bin").
func parseOutFlag(args []string) (out string, rest []string, err error) {
	out = "bin"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--out":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--out requires a directory")
			}
			out = args[i+1]
			i++
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		default:
			rest = append(rest, a)
		}
	}
	return out, rest, nil
}
```

In `buildCmd`, resolve `out`/`rest`, then:

```go
	outFile := filepath.Join(out, mod+".erl")
	if _, err := os.Stat(outFile); err == nil {
		return fmt.Errorf("%s already exists (refusing to overwrite; use --out or remove it)", outFile)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outFile, []byte(erl), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, outFile)
```

Apply the same `--out`/collision handling in `runCmd` (compile/boot from the chosen dir).

- [ ] **Step 4: Convert the leftover-`bin/` tests to `os.Chdir(t.TempDir())`**

In `TestBuildCommand` and `TestRunAssemblesErlcAndErl`, add `os.Chdir(t.TempDir())` at the top and drop the trailing `os.Remove(...)` cleanup — the temp dir is discarded automatically, so no empty `bin/` is left in the package directory.

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS.

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): add --out flag and output collision guard (C1)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 11 (C3): Wire `wm run` stdout ("booting <mod>")

**Files:**
- Modify: `internal/pkg/cli/cli.go` (`runCmd`)
- Test: `internal/pkg/cli/cli_test.go` (extend `TestRunAssemblesErlcAndErl`)

**Interfaces:** no signature change; `runCmd` now writes `booting <mod>` to stdout.

- [ ] **Step 1: Extend the test (red)**

In `TestRunAssemblesErlcAndErl`, assert on `out`:

```go
	if !strings.Contains(out.String(), "booting m") {
		t.Fatalf("stdout = %q, want 'booting m'", out.String())
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/cli/ -run TestRunAssemblesErlcAndErl -v`
Expected: FAIL — nothing written to stdout.

- [ ] **Step 3: Write minimal implementation**

In `runCmd`, after `mod` is known and before the erlc call:

```go
	fmt.Fprintf(stdout, "booting %s\n", mod)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/cli/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/cli/
git commit -s -m "feat(cli): print 'booting <mod>' from wm run (C3)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 12 (B2): Verify OTP tarball against a pinned SHA-256

**Files:**
- Modify: `internal/pkg/erlang/source.go` (pinned-hash map)
- Modify: `internal/pkg/erlang/build.go` (`fetchSource`, `Provision`)
- Test: `internal/pkg/erlang/build_test.go`
- Modify: `docs/verified-sources.md` (record the pin)

**Interfaces:**
- Produces: **`fetchSource(ctx context.Context, url, wantSHA string) ([]byte, error)`** — downloads `url`, verifies its SHA-256 equals `wantSHA`, returns the bytes. `Provision` uses it and pipes the verified bytes into `tar` via `bytes.NewReader`.
- Consumes: `SourceURL(version)`, new `sourceSHA256` map.

- [ ] **Step 1: Write the failing test (offline, httptest)**

```go
func TestFetchSourceVerifiesSHA(t *testing.T) {
	body := []byte("fake tarball bytes")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	got, err := fetchSource(context.Background(), srv.URL, good)
	if err != nil {
		t.Fatalf("matching sha should pass: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %q", got)
	}
	if _, err := fetchSource(context.Background(), srv.URL, "deadbeef"); err == nil {
		t.Fatal("mismatched sha should error, got nil")
	}
}
```

Add imports: `crypto/sha256`, `encoding/hex`, `net/http`, `net/http/httptest`, `context`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestFetchSourceVerifiesSHA -v`
Expected: FAIL — `fetchSource` undefined.

- [ ] **Step 3: Write `fetchSource` and the pinned map**

In `source.go`:

```go
// sourceSHA256 pins the SHA-256 of each supported OTP source tarball. Verified
// before every build (stdlib crypto/sha256; no third-party sigstore verifier).
var sourceSHA256 = map[string]string{
	"29.0.3": "PASTE_REAL_HASH_HERE",
}
```

In `build.go`:

```go
func fetchSource(ctx context.Context, url, wantSHA string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	// ponytail: ReadAll the whole tarball (~64 MiB) into memory for a one-shot
	// install so the hash can be verified before any extraction. Stream-to-temp
	// only if memory pressure ever matters.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantSHA {
		return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSHA)
	}
	return data, nil
}
```

Rewrite `Provision` to use it: look up `want, ok := sourceSHA256[version]` (error if absent), call `data, err := fetchSource(ctx, SourceURL(version), want)`, then `tarCmd.Stdin = bytes.NewReader(data)` instead of `resp.Body`. Remove the now-dead inline `http.NewRequest`/`resp` block. Add imports `bytes`, `crypto/sha256`, `encoding/hex`.

- [ ] **Step 4: Pin the real hash**

Run:

```bash
curl -sL https://github.com/erlang/otp/releases/download/OTP-29.0.3/otp_src_29.0.3.tar.gz | sha256sum
```

Paste the 64-hex digest into `sourceSHA256["29.0.3"]`, replacing `PASTE_REAL_HASH_HERE`. Add a row/note to `docs/verified-sources.md` recording the pinned SHA-256 and the date.

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./internal/pkg/erlang/ -v`
Expected: PASS.

```bash
git add internal/pkg/erlang/ docs/verified-sources.md
git commit -s -m "feat(erlang): verify OTP tarball against pinned SHA-256 (B2)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 13 (B3): Download timeout, retry, and size ceiling

**Files:**
- Modify: `internal/pkg/erlang/build.go` (`fetchSource`)
- Test: `internal/pkg/erlang/build_test.go`

**Interfaces:** `fetchSource` gains a bounded client timeout, a small retry, and a size cap. Signature unchanged.

- [ ] **Step 1: Write the failing tests**

```go
func TestFetchSourceSizeCeiling(t *testing.T) {
	big := make([]byte, (200<<20)+1) // one byte over the ceiling
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big)
	}))
	defer srv.Close()
	if _, err := fetchSource(context.Background(), srv.URL, "irrelevant"); err == nil {
		t.Fatal("want error when body exceeds the size ceiling, got nil")
	}
}

func TestFetchSourceRetries(t *testing.T) {
	body := []byte("ok")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	got, err := fetchSource(context.Background(), srv.URL, want)
	if err != nil || string(got) != "ok" {
		t.Fatalf("retry should succeed on 2nd attempt: got %q err %v", got, err)
	}
	if calls < 2 {
		t.Fatalf("expected a retry, calls = %d", calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/erlang/ -run 'TestFetchSource(SizeCeiling|Retries)' -v`
Expected: FAIL — no cap, no retry (first 500 is returned as an error).

- [ ] **Step 3: Harden `fetchSource`**

```go
const maxSourceBytes = 200 << 20 // 200 MiB ceiling; OTP src is ~64 MiB

func fetchSource(ctx context.Context, url, wantSHA string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		data, err := fetchOnce(ctx, client, url)
		if err != nil {
			lastErr = err
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != wantSHA {
			return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSHA)
		}
		return data, nil
	}
	return nil, fmt.Errorf("download %s failed after 3 attempts: %w", url, lastErr)
}

func fetchOnce(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSourceBytes {
		return nil, fmt.Errorf("source exceeds %d bytes ceiling", maxSourceBytes)
	}
	return data, nil
}
```

Add `import "time"`. A checksum mismatch is not retried (it is deterministic); only transport/HTTP failures retry.

- [ ] **Step 4: Run the full suite and commit**

Run: `go test ./internal/pkg/erlang/ -v`
Expected: PASS.

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): add download timeout, retry, and size ceiling (B3)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 14 (B5): `Installed()` also requires `erlc`

**Files:**
- Modify: `internal/pkg/erlang/toolchain.go` (`Installed`)
- Test: `internal/pkg/erlang/toolchain_test.go` (extend), and fix `internal/pkg/cli/cli_test.go` `TestErlangList` (writes only `erl`)

**Interfaces:** `Installed()` returns true only when both `erl` and `erlc` exist.

- [ ] **Step 1: Extend the test (red)**

In `TestInstalled`, after writing only `erl`, assert it is NOT yet installed, then write `erlc` and assert installed:

```go
	if err := os.WriteFile(l.Erl(), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if l.Installed() {
		t.Fatal("should not be installed with erl but no erlc")
	}
	if err := os.WriteFile(l.Erlc(), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !l.Installed() {
		t.Fatal("should be installed with both erl and erlc")
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestInstalled -v`
Expected: FAIL — current `Installed()` returns true with only `erl`.

- [ ] **Step 3: Write minimal implementation**

```go
// Installed reports whether both erl and erlc exist in this layout's bin.
func (l Layout) Installed() bool {
	for _, p := range []string{l.Erl(), l.Erlc()} {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Fix the cli list test regression**

In `cli_test.go` `TestErlangList`, also write an `erlc` file so the version still lists:

```go
	os.WriteFile(filepath.Join(home, ".local", "erlang", "29.0.3", "bin", "erlc"), []byte("x"), 0o755)
```

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./...`
Expected: PASS.

```bash
git add internal/pkg/erlang/ internal/pkg/cli/
git commit -s -m "fix(erlang): require both erl and erlc for Installed (B5)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 15 (B6): Probe `tar` capability before extraction

**Files:**
- Modify: `internal/pkg/erlang/build.go` (`Provision`, add `tarSupportsStrip` + probe)
- Test: `internal/pkg/erlang/build_test.go`

**Interfaces:**
- Produces: **`tarSupportsStrip(versionOutput string) bool`** — pure classifier over `tar --version` output (GNU tar / bsdtar supported). `Provision` runs `tar --version` and fails clearly if unsupported.

- [ ] **Step 1: Write the failing test (pure classifier)**

```go
func TestTarSupportsStrip(t *testing.T) {
	yes := []string{"tar (GNU tar) 1.35", "bsdtar 3.5.3 - libarchive 3.5.3"}
	no := []string{"BusyBox v1.36.1", "tar: unknown", ""}
	for _, v := range yes {
		if !tarSupportsStrip(v) {
			t.Fatalf("tarSupportsStrip(%q) = false, want true", v)
		}
	}
	for _, v := range no {
		if tarSupportsStrip(v) {
			t.Fatalf("tarSupportsStrip(%q) = true, want false", v)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/erlang/ -run TestTarSupportsStrip -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write the classifier and wire the probe**

```go
// tarSupportsStrip reports whether `tar --version` output identifies a tar that
// supports --strip-components (GNU tar or bsdtar). BusyBox tar does not.
func tarSupportsStrip(versionOutput string) bool {
	return strings.Contains(versionOutput, "GNU tar") || strings.Contains(versionOutput, "bsdtar")
}
```

In `Provision`, before the extraction `tarCmd`:

```go
	verOut, err := exec.CommandContext(ctx, "tar", "--version").Output()
	if err != nil {
		return fmt.Errorf("tar not available: %w", err)
	}
	if !tarSupportsStrip(string(verOut)) {
		return fmt.Errorf("system tar does not support --strip-components (need GNU tar or bsdtar)")
	}
```

- [ ] **Step 4: Run the full suite and commit**

Run: `go test ./internal/pkg/erlang/ -v`
Expected: PASS.

```bash
git add internal/pkg/erlang/
git commit -s -m "feat(erlang): probe tar --strip-components capability (B6)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 16 (C5): Clear transpile-only guard message for `pkg/otp`

**Files:**
- Modify: `pkg/otp/otp.go`
- Test: `pkg/otp/otp_test.go`

**Interfaces:**
- Produces: shared `transpileOnly(sym string)` helper; markers panic with an actionable, symbol-named message. (A full build-tag guard is rejected — it would hide the marker symbols and break the "Wintermute source is valid Go / stock Go tooling works" thesis; recorded as a code comment.)

- [ ] **Step 1: Extend the test (red)**

```go
func TestMarkerMessageNamesFix(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "wm build") || !strings.Contains(msg, "Spawn") {
			t.Fatalf("panic message should name the symbol and the fix, got: %v", r)
		}
	}()
	_ = Spawn(func() {})
}
```

Add `import "strings"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/otp/ -run TestMarkerMessageNamesFix -v`
Expected: FAIL — current message is a fixed string without the symbol or `wm build`.

- [ ] **Step 3: Write minimal implementation**

```go
// transpileOnly panics with an actionable message naming the marker and the
// fix. These markers exist so the source is valid Go (gopls/go vet work); they
// are meant to be transpiled, not run.
// ponytail: a //go:build guard would fail earlier but hides the symbols and
// breaks the valid-Go tooling thesis — rejected deliberately.
func transpileOnly(sym string) {
	panic("otp." + sym + ": transpile-only marker — compile with `wm build`, do not run natively")
}

func Self() Pid                   { transpileOnly("Self"); return Pid{} }
func Spawn(fn func()) Pid         { transpileOnly("Spawn"); return Pid{} }
func Register(name string, p Pid) { transpileOnly("Register") }
func Whereis(name string) Pid     { transpileOnly("Whereis"); return Pid{} }
func Send(to Pid, msg any)        { transpileOnly("Send") }
func Receive() any                { transpileOnly("Receive"); return nil }
func Print(s string)              { transpileOnly("Print") }
```

Remove the old `transpileOnly` string const.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/otp/ -v`
Expected: PASS (both the existing panic test and the new message test).

- [ ] **Step 5: Commit**

```bash
git add pkg/otp/
git commit -s -m "feat(otp): actionable transpile-only guard message (C5)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 17: Verification gate (security scan, real build, Copilot review)

**Files:** none (verification only; fixes land as follow-up commits if the gate finds issues).

- [ ] **Step 1: Full unit suite**

Run: `go build -o bin/wm ./cmd/wm && go test ./...`
Expected: all green.

- [ ] **Step 2: Security tooling** (B1/B2 are their domain)

```bash
govulncheck ./...
gosec ./...
gitleaks detect
~/.python/venv/wintermute/bin/semgrep --config auto
```
Expected: no new findings. Triage or fix anything reported (commit fixes separately).

- [ ] **Step 3: Real toolchain build + integration ladder** (memory: green units are not enough)

```bash
./bin/wm erlang install     # rebuilds OTP 29.0.3 with SHA-256 verification now active
go test -tags integration ./internal/pkg/ladder/
```
Expected: install verifies the pinned hash and builds; ladder rungs 1–4 PASS. This confirms B2's pinned hash is correct and B5/B6 did not regress provisioning.

- [ ] **Step 4: Copilot review gate** (before any github-bound commit)

```bash
gh copilot -- -p "Review the staged git diff for correctness, DRY/KISS, and Go best practices." --allow-all-tools
```
Address findings, then the branch is ready to squash-merge per the git workflow.

- [ ] **Step 5: Update HANDOVER.md**

Mark the 0.2.0 hardening step complete; note the 0.2.x line's next step (distributed interop). Commit.

---

## Self-Review

**Spec coverage:** A1✓T1 A2✓T2 A3✓T3 A4✓T4 A5✓T5 · B1✓T9 B2✓T12 B3✓T13 B4✓T8 B5✓T14 B6✓T15 · C1✓T10 C2✓T7 C3✓T11 C4✓T6 C5✓T16. Cross-cutting: VERSION bump (done), security tooling + real build + Copilot (T17). All 16 spec items plus the verification gate are covered.

**Placeholder scan:** The only deferred value is B2's real SHA-256 (T12 Step 4), pinned via an exact command — a determinate fact, not a vague TODO. The unit test verifies the logic offline; the real hash is confirmed by T17 Step 3.

**Type consistency:** `File(src) (string, string, error)` (T7) is used consistently by cli after T7. `fetchSource(ctx, url, wantSHA)` introduced in T12, extended in T13 (same signature). `ValidateVersion`/`parseVersionFlag`/`parseOutFlag`/`tarSupportsStrip`/`transpileOnly` are each defined once and referenced consistently.

**Ordering note:** T7 changes `File`'s signature; T1–T6 use the old 2-value form, so run them before T7 as numbered. T8 (`parseVersionFlag`) precedes T9 which calls it. T12 introduces `fetchSource`; T13 hardens it.
