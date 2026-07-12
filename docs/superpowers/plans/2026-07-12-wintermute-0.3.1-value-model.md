# Wintermute 0.3.1 — Value Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Go→Erlang transpiler a value model — function parameters, a trailing `return`, local bindings (`:=`), and calls with arguments — without introducing control flow.

**Architecture:** Approach A, minimal statement-set extension of the existing flat comma-sequence emitter in `internal/pkg/transpile/transpile.go`. Add two `emitStmt` cases (`:=`, trailing `return`), emit call arguments, fix export arity to `f/N`, and track a per-function `bound` name set to reject Erlang-illegal rebinding. No tree abstraction (that is 0.3.2's job when `if`/`case` needs it).

**Tech Stack:** Go standard library only (`go/ast`, `go/parser`, `go/token`). Erlang/OTP 29.0.3 `erlc` for the real-toolchain rung.

## Global Constraints

- **Stdlib only. No third-party modules.** (project rule)
- **TDD, red → green:** write the failing test, watch it fail, then implement. Tests before implementation, always.
- **Module path is `go.muehmer.eu/wintermute`** — not the GitHub repo path.
- **Deterministic output:** emitter ordering is already stabilized in `Module`; do not introduce map-order-dependent output.
- **Build to `bin/`:** `go build -o bin/wm ./cmd/wm`, never bare `go build`.
- **Erlang-variable casing rule:** identifiers that become Erlang variables (parameters, `:=` targets, field binds) must be uppercase-leading; reject lowercase with a positioned error — never auto-capitalize.
- **No transpiler automatism beyond a clean 1:1 Erlang mapping.** Constructs without one (early return, operators other than `+`, `if`/`case`) keep erroring, pointing at the 0.3.2 roadmap where useful.

Reference: spec at `docs/superpowers/specs/2026-07-12-wintermute-0.3.1-value-model-design.md`.

Test commands:
- Unit: `go test ./internal/pkg/transpile/`
- Full: `go test ./...`
- Integration rung: `go test -tags integration ./internal/pkg/ladder/`

---

### Task 1: Function parameters + export arity

Drop the nullary-only restriction on function declarations. A function's parameters become uppercase Erlang variables in the clause head; exported functions export as `f/N`. Parameter names are seeded into a new per-function `bound` set (used by later tasks) so the emitter knows which identifiers are already Erlang variables.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (the `emitter` struct ~lines 20-24; `Module` param-rejection ~lines 105-116)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: existing `emitter{structs, fset, registered}`, `Module(src) (Result, error)`, `em.emitBody`.
- Produces:
  - `emitter` gains field `bound map[string]bool` (reset/allocated per function in `Module`, seeded with that function's parameter names).
  - `paramNames(fn *ast.FuncDecl) ([]string, error)` — helper returning the ordered parameter names (flattening grouped params like `X, Y int`), erroring on a lowercase-leading name via `em.errorf`.
  - Exported functions export as `<lowername>/<len(params)>`.

- [ ] **Step 1: Write the failing tests**

Add to `transpile_test.go`. Both tests must be green at the end of Task 1, so the parameterized function under test uses a bare `otp.Print` body (no `return` — that is Task 2):

```go
func TestModule_ParamHeadAndArity(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
func Greet(Name string) { otp.Print(Name) }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "-export([greet/1]).") {
		t.Errorf("want export greet/1, got:\n%s", r.Erl)
	}
	if !strings.Contains(r.Erl, "greet(Name) ->") {
		t.Errorf("want clause head greet(Name), got:\n%s", r.Erl)
	}
}

func TestModule_LowercaseParamRejected(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
func Greet(name string) { otp.Print(name) }`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "uppercase") {
		t.Fatalf("want uppercase-param error, got %v", err)
	}
}
```

The parameter+`return` combination (`add(X, Y) -> X + Y`) is covered by `TestModule_TrailingReturn` in Task 2 — do not add a `return`-using test here, it would commit red.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'ParamHeadAndArity|LowercaseParamRejected' -v`
Expected: FAIL — `TestModule_ParamHeadAndArity` fails at the current `parameters are not yet supported` error; `TestModule_LowercaseParamRejected` fails because that error mentions "not yet supported", not "uppercase".

- [ ] **Step 3: Add the `bound` field and `paramNames` helper**

Add the field to the `emitter` struct:

```go
type emitter struct {
	structs    map[string][]string
	fset       *token.FileSet
	registered []string
	bound      map[string]bool
}
```

Add the helper (near `receiverTypeName`):

```go
// paramNames returns the ordered parameter names of fn, flattening grouped
// declarations (X, Y int -> [X, Y]). Each name becomes an Erlang variable, so a
// lowercase-leading name is rejected (never auto-capitalized).
func (em *emitter) paramNames(fn *ast.FuncDecl) ([]string, error) {
	var names []string
	if fn.Type.Params == nil {
		return names, nil
	}
	for _, fld := range fn.Type.Params.List {
		for _, n := range fld.Names {
			if !token.IsExported(n.Name) {
				return nil, em.errorf(n, "parameter %s is lowercase-leading; Erlang variables must be uppercase", n.Name)
			}
			names = append(names, n.Name)
		}
	}
	return names, nil
}
```

- [ ] **Step 4: Emit parameterized heads and correct arity in `Module`**

Replace the nullary rejection block (currently):

```go
		if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
			return Result{}, fmt.Errorf("unsupported function %s: parameters are not yet supported (echo subset); see the 0.2.x roadmap", fn.Name.Name)
		}
		name := strings.ToLower(fn.Name.Name)
```

with:

```go
		params, err := em.paramNames(fn)
		if err != nil {
			return Result{}, err
		}
		name := strings.ToLower(fn.Name.Name)
```

Seed `bound` per function, immediately before the `em.emitBody(fn.Body)` call:

```go
		em.bound = map[string]bool{}
		for _, p := range params {
			em.bound[p] = true
		}
		stmts, err := em.emitBody(fn.Body)
```

Update the export to use arity, replacing `exports = append(exports, name+"/0")`:

```go
		if fn.Name.IsExported() {
			exports = append(exports, fmt.Sprintf("%s/%d", name, len(params)))
		}
```

Emit the clause head with parameters. Replace the two `Fprintf` head-emission lines (the one-line and multi-line clause forms) so the head is `name(P1, P2, ...)`:

```go
		head := name + "(" + strings.Join(params, ", ") + ")"
		if fn.Body != nil && len(fn.Body.List) == 1 && !strings.Contains(stmts, "\n") {
			fmt.Fprintf(&bodies, "\n%s -> %s.\n", head, stmts)
		} else {
			fmt.Fprintf(&bodies, "\n%s ->\n%s.\n", head, indent(stmts))
		}
```

- [ ] **Step 5: Run the gating tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'ParamHeadAndArity|LowercaseParamRejected' -v`
Expected: PASS both.

- [ ] **Step 6: Run the full transpile suite for regressions**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS — all tests green (Task 1 adds no `return`-dependent test). If any previously-passing test broke, fix before committing.

- [ ] **Step 7: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): function parameters and f/N export arity"
```

---

### Task 2: Trailing `return`

Honour `return expr` as the last statement of a function body, emitting `expr` as the trailing value. A `return` anywhere but the last position is rejected (early return needs control flow, 0.3.2).

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmt` ~lines 351-358; `emitBody`/`emitStmts` path)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitter.bound` and parameterized heads from Task 1; `em.emitExpr`, `em.emitStmts`.
- Produces: `emitStmt` handles `*ast.ReturnStmt` (single result expression → the expression's Erlang text). Non-last `return` and multi-result `return` error via `em.errorf`.

- [ ] **Step 1: Write the failing tests**

```go
func TestModule_TrailingReturn(t *testing.T) {
	src := `package math
func Add(X, Y int) int { return X + Y }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "add(X, Y) -> X + Y.") {
		t.Errorf("want add(X, Y) -> X + Y, got:\n%s", r.Erl)
	}
}

func TestModule_EarlyReturnRejected(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
func F(X int) int { return X
	otp.Print("unreached") }`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "case") {
		t.Fatalf("want early-return error pointing at case/0.3.2, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'TrailingReturn|EarlyReturnRejected' -v`
Expected: FAIL — `emitStmt` currently returns `unsupported statement: *ast.ReturnStmt`.

- [ ] **Step 3: Handle `return` position in `emitStmts`**

`emitStmts` is where statement position is known. Add a check so a `*ast.ReturnStmt` is only allowed as the final element, and emit its single result via `emitExpr`. Replace `emitStmts` with:

```go
// emitStmts emits a list of statements as a comma-separated Erlang expression
// sequence. A return statement is only valid as the final element (Erlang has
// no early return); a non-final return is rejected.
func (em *emitter) emitStmts(list []ast.Stmt) (string, error) {
	var parts []string
	for i, s := range list {
		if ret, ok := s.(*ast.ReturnStmt); ok {
			if i != len(list)-1 {
				return "", em.errorf(ret, "early return is unsupported; needs case/if (0.3.2)")
			}
			if len(ret.Results) != 1 {
				return "", em.errorf(ret, "return must yield exactly one value (multi-value return is 0.3.2+)")
			}
			e, err := em.emitExpr(ret.Results[0])
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			continue
		}
		e, err := em.emitStmt(s)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ",\n"), nil
}
```

Leave `emitStmt`'s `default` as-is; `return` is now handled before `emitStmt` is reached.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'TrailingReturn|EarlyReturnRejected' -v`
Expected: PASS both.

- [ ] **Step 5: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS. The receive path routes through `emitStmts` for its clause body — confirm the existing receive tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): trailing return as the function value"
```

---

### Task 3: Local bindings (`:=`) with immutability + rebinding guards

`Z := expr` becomes the Erlang match `Z = expr`. The bound name must be uppercase; re-assignment (`z = ...`) and rebinding an already-bound name are rejected — both would produce Erlang that fails at runtime.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmt` switch)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `emitter.bound` (seeded with params in Task 1), `em.emitExpr`.
- Produces: `emitStmt` handles `*ast.AssignStmt`. `token.DEFINE` (`:=`) with a single uppercase LHS ident → `Name = <expr>` and records the name in `bound`. `token.ASSIGN` (`=`) → immutability error. Rebinding a name already in `bound` → error. Note: the receive-assign form (`x := otp.Receive().(T)`) is still intercepted earlier in `emitBody`, so it never reaches this case.

- [ ] **Step 1: Write the failing tests**

```go
func TestModule_LocalBinding(t *testing.T) {
	src := `package math
func Add(X, Y int) int {
	Z := X + Y
	return Z
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "Z = X + Y") {
		t.Errorf("want binding Z = X + Y, got:\n%s", r.Erl)
	}
	if !strings.Contains(r.Erl, "Z = X + Y,\n    Z.") {
		t.Errorf("want Z bound then returned, got:\n%s", r.Erl)
	}
}

func TestModule_ReassignmentRejected(t *testing.T) {
	src := `package math
func F(X int) int {
	X = X + 1
	return X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("want immutability error, got %v", err)
	}
}

func TestModule_RebindingRejected(t *testing.T) {
	src := `package math
func F(X int) int {
	X := X
	return X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want already-bound error, got %v", err)
	}
}

func TestModule_LowercaseBindingRejected(t *testing.T) {
	src := `package math
func F(X int) int {
	z := X
	return z
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "uppercase") {
		t.Fatalf("want uppercase error, got %v", err)
	}
}
```

Note: Go itself rejects `X := X` (no new variable on the left of `:=`) only when NO name on the LHS is new; with a single name that shadows a param, `go/parser` still parses it (semantic check is `go/types`, which the transpiler does not run). `parser.ParseFile` with mode `0` does not type-check, so these sources parse and reach the emitter. Confirm in Step 2 that the parse succeeds and the emitter is what rejects them.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'LocalBinding|ReassignmentRejected|RebindingRejected|LowercaseBindingRejected' -v`
Expected: FAIL — `emitStmt` returns `unsupported statement: *ast.AssignStmt` for all four (or a parse error surfaces; if a parse error appears instead of an emitter error, adjust the test source to a form `go/parser` accepts, e.g. use distinct names, and re-run).

- [ ] **Step 3: Handle `*ast.AssignStmt` in `emitStmt`**

Add a case to the `emitStmt` switch, before `default`:

```go
	case *ast.AssignStmt:
		if st.Tok == token.ASSIGN {
			return "", em.errorf(st, "re-assignment is unsupported; Erlang variables are immutable (single-assignment only)")
		}
		if st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
			return "", em.errorf(st, "only single-name := bindings are supported")
		}
		id, ok := st.Lhs[0].(*ast.Ident)
		if !ok {
			return "", em.errorf(st, "binding target must be a plain identifier")
		}
		if !token.IsExported(id.Name) {
			return "", em.errorf(st, "binding %s is lowercase-leading; Erlang variables must be uppercase", id.Name)
		}
		if em.bound[id.Name] {
			return "", em.errorf(st, "%s is already bound; Erlang has no rebinding", id.Name)
		}
		rhs, err := em.emitExpr(st.Rhs[0])
		if err != nil {
			return "", err
		}
		em.bound[id.Name] = true
		return id.Name + " = " + rhs, nil
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'LocalBinding|ReassignmentRejected|RebindingRejected|LowercaseBindingRejected' -v`
Expected: PASS all four.

- [ ] **Step 5: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): local := bindings with immutability and rebinding guards"
```

---

### Task 4: Calls with arguments

Bare-identifier calls to same-module functions may now take arguments: `f(A, B)` → `f(A, B)`. This removes the nullary-only guard and unblocks self-recursion (Erlang applies last-call optimization for free — nothing to implement for tail calls beyond emitting the call).

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitCall` bare-ident branch ~lines 438-443)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `em.emitExpr` for argument expressions.
- Produces: `emitCall`'s bare-`*ast.Ident` branch emits `<lowername>(<arg1>, <arg2>, ...)`. The `otp.*` selector-call path is unchanged.

- [ ] **Step 1: Write the failing tests**

```go
func TestModule_CallWithArgs(t *testing.T) {
	src := `package math
func Double(X int) int { return Add(X, X) }
func Add(X, Y int) int { return X + Y }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "double(X) -> add(X, X).") {
		t.Errorf("want double(X) -> add(X, X), got:\n%s", r.Erl)
	}
}

func TestModule_SelfRecursionEmits(t *testing.T) {
	// Recursion mechanism only; a real base case needs case/if (0.3.2).
	src := `package loop
func Spin(X int) int { return Spin(X) }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "spin(X) -> spin(X).") {
		t.Errorf("want spin(X) -> spin(X), got:\n%s", r.Erl)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run 'CallWithArgs|SelfRecursionEmits' -v`
Expected: FAIL — the bare-ident call branch currently errors `only nullary self-calls are in the subset`.

- [ ] **Step 3: Emit arguments in the bare-ident call branch**

Replace the bare-ident branch in `emitCall` (currently):

```go
	if id, ok := c.Fun.(*ast.Ident); ok {
		if len(c.Args) != 0 {
			return "", em.errorf(c, "unsupported call %s with arguments: only nullary self-calls are in the subset (see the 0.2.x roadmap)", id.Name)
		}
		return strings.ToLower(id.Name) + "()", nil
	}
```

with:

```go
	if id, ok := c.Fun.(*ast.Ident); ok {
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			s, err := em.emitExpr(a)
			if err != nil {
				return "", err
			}
			args[i] = s
		}
		return strings.ToLower(id.Name) + "(" + strings.Join(args, ", ") + ")", nil
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run 'CallWithArgs|SelfRecursionEmits' -v`
Expected: PASS both.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS all packages.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): calls with arguments (enables self-recursion emission)"
```

---

### Task 5: Real-toolchain rung — transpile + `erlc` compile

Prove the emitted Erlang is valid, not just that the strings match. Add a testdata fixture exercising parameters + a local binding + a call-with-args, and an integration-tagged rung that transpiles it and compiles the result with `erlc`, reusing the existing `transpileToErl` helper and `erlang.Layout`. Per the `run-real-toolchain-build-early` memory: green unit tests are not enough.

**Files:**
- Create: `testdata/valuemodel/math.go` (Go fixture; `testdata/` is skipped by the go tool, so a stray `package math` here is not compiled by `go test ./...`)
- Modify: `internal/pkg/ladder/ladder_integration_test.go` (add the rung next to the existing rungs — it reuses `transpileToErl` and the layout pattern defined in that file)

**Interfaces:**
- Consumes (both already in `ladder_integration_test.go`):
  - `transpileToErl(t *testing.T, goPath, dir string) string` — transpiles the Go fixture and writes `<dir>/<mod>.erl`, returning the written path.
  - the layout pattern: `home, _ := os.UserHomeDir()`; `l := erlang.NewLayout(home, erlang.DefaultVersion)`; skip on `!l.Installed()`; compile via `l.Erlc()`.
- Produces: a green rung `TestRung_ValueModelCompiles` under `-tags integration ./internal/pkg/ladder/`.

- [ ] **Step 1: Write the fixture**

Create `testdata/valuemodel/math.go`:

```go
// Package math is a 0.3.1 value-model fixture: parameters, a local binding,
// and a call with arguments. It transpiles and must compile with erlc.
package math

// Add returns the sum of X and Y.
func Add(X, Y int) int { return X + Y }

// Double returns X + X via a local binding and a call with arguments.
func Double(X int) int {
	Z := Add(X, X)
	return Z
}
```

- [ ] **Step 2: Write the failing rung**

Append to `internal/pkg/ladder/ladder_integration_test.go` (the imports `os`, `os/exec`, `path/filepath`, `testing`, and `erlang` are already present in that file):

```go
// TestRung_ValueModel transpiles the 0.3.1 value-model fixture (parameters,
// a local binding, a call with arguments) and proves the emitted Erlang
// actually compiles with erlc — green unit tests are not enough.
func TestRung_ValueModel(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	dir := t.TempDir()
	erl := transpileToErl(t, filepath.FromSlash("../../../testdata/valuemodel/math.go"), dir)
	if out, err := exec.Command(l.Erlc(), "-o", dir, erl).CombinedOutput(); err != nil {
		t.Fatalf("erlc %s: %v\n%s", erl, err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "math.beam")); err != nil {
		t.Fatalf("no math.beam produced: %v", err)
	}
}
```

- [ ] **Step 3: Run the rung — fail for the right reason, then pass**

If `erlc` is not installed, provision first: `./bin/wm erlang install`.

Run: `go test -tags integration ./internal/pkg/ladder/ -run ValueModel -v`
Expected: PASS — the emitter changes from Tasks 1-4 already produce valid Erlang, and `math.beam` is produced. If `erlc` reports a **syntax error**, that is a real defect in the emitted Erlang — fix the emitter (a failing unit test should be added to reproduce it), not the rung. A wrong fixture path or a missing helper is the only other early failure; fix that.

- [ ] **Step 4: Commit**

```bash
git add testdata/valuemodel/math.go internal/pkg/ladder/ladder_integration_test.go
git commit -s -m "test(transpile): real-toolchain rung — value-model fixture compiles with erlc"
```

---

### Task 6: Verification gate + docs refresh

Run the full verification gate, refresh the SDK-index note if needed (no `pkg/` change, so likely unchanged), and update `HANDOVER.md` for the next session. No new code.

**Files:**
- Modify: `HANDOVER.md`
- Verify only: `docs/SDK-INDEX.md` (expected unchanged — no `pkg/otp` change)

- [ ] **Step 1: Full unit + build gate**

Run:
```bash
go build -o bin/wm ./cmd/wm
go test ./...
```
Expected: build clean, all packages green.

- [ ] **Step 2: Integration gate**

Run:
```bash
go test -tags integration ./internal/pkg/ladder/
go test -tags integration ./internal/pkg/cli/
```
Expected: all rungs green, including the new value-model rung. If leftover BEAM nodes cause an odd failure, clear with `pkill -9 -x beam.smp; pkill -9 -x epmd` and re-run (see the `integration-test-leftover-nodes` memory).

- [ ] **Step 3: Security sweep (baseline check, no new findings expected)**

Run:
```bash
govulncheck ./...
gosec ./...
gitleaks detect
```
Expected: `govulncheck`/`gitleaks` clean; `gosec` findings unchanged in category from the 0.3.0 baseline (accepted G703 class only). A NEW unaccepted HIGH/CRITICAL category must be triaged before release.

- [ ] **Step 4: Update the handover**

Update `HANDOVER.md`: 0.3.1 delivered (value model — parameters, trailing return, `:=` bindings, calls-with-args, arity export, erlc rung), the verification-gate results with the run date, and set the next step to 0.3.2 (operators + `if`/`case`/`switch`). Move the guard open-question note forward.

- [ ] **Step 5: Commit**

```bash
git add HANDOVER.md docs/SDK-INDEX.md
git commit -s -m "docs: handover — 0.3.1 value model complete, gate green"
```

---

## Notes for the implementer

- **Release/merge is out of this plan's scope.** These tasks land on a working branch on `origin`. Promotion to `main`, tagging `v0.3.1`, the Copilot gate, pushing to the gated remotes, and the GitHub release are the finishing flow (`superpowers:finishing-a-development-branch` + `CLAUDE.md`), done after the plan is verified green — not inside Task 6.
- **Erlang casing is a hard rule, not a nicety.** Every new rejection (lowercase param, lowercase binding) exists because emitting a lowercase name would silently produce an Erlang atom instead of a variable — a wrong program that compiles. Do not "helpfully" auto-capitalize.
- **The receive-assign special case still lives in `emitBody`** and is matched before `emitStmts`/`emitStmt`. Task 3's `*ast.AssignStmt` case handles only the non-receive `:=`. Do not move the receive interception.
