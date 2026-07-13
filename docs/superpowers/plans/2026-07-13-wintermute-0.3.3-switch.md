# Wintermute 0.3.3 — Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the tagged expression `switch` → Erlang `case`-on-value to the transpiler, reusing the 0.3.2 `case`/branch-scoping machinery.

**Architecture:** Extend the existing emitter in `internal/pkg/transpile/transpile.go`, mirroring `emitIf`. A `*ast.SwitchStmt` in tail position emits `case <Tag> of V -> clause; … ; _ -> default end`. Only the tagged form with single literal case values and a required `default` is supported; each clause is emitted in its own `bound` scope via the existing `emitBranch`. A type switch (`*ast.TypeSwitchStmt`) is a distinct node, rejected in `emitStmt`.

**Tech Stack:** Go standard library only (`go/ast`, `go/token`). Erlang/OTP 29.0.3 `erlc`/`erl` for the runnable rung.

## Global Constraints

- **Stdlib only. No third-party modules.**
- **TDD, red → green:** write the failing test, watch it fail, then implement.
- **Module path is `go.muehmer.eu/wintermute`.**
- **`bound-set-integration` invariant:** switch clauses are a binding context — each clause body is emitted via `emitBranch` (snapshot/restore of `em.bound`), so sibling clauses reuse a name freshly while an outer collision is rejected. Case literals bind no names. Test sibling-reuse AND outer-collision.
- **No transpiler automatism beyond a clean 1:1 Erlang mapping.** Deferred constructs (no-default switch, multi-value cases, tagless, type switch, fallthrough) must error, pointing at 0.3.4+ where useful.
- **Empty clause bodies are rejected** — an empty case/default body would emit an invalid `V -> ;` clause (the exact bug class the 0.3.2 Copilot gate caught).
- **Deterministic output** (clauses in source order, `default` last); **build to `bin/`**.

Reference: spec at `docs/superpowers/specs/2026-07-13-wintermute-0.3.3-switch-design.md`.

Test commands: unit `go test ./internal/pkg/transpile/`; full `go test ./...`; integration `go test -tags integration ./internal/pkg/ladder/`.

---

### Task 1: `switch` → Erlang `case` (emitSwitch + wiring)

Handle `*ast.SwitchStmt` in `emitStmts` (tail position, terminal). Add `emitSwitch`. Reject the deferred forms and empty clauses. Reject `*ast.TypeSwitchStmt` in `emitStmt`.

**Files:**
- Modify: `internal/pkg/transpile/transpile.go` (`emitStmts` loop; add `emitSwitch` + a `switchFallthrough` helper; `emitStmt` add a `*ast.TypeSwitchStmt` case)
- Test: `internal/pkg/transpile/transpile_test.go`

**Interfaces:**
- Consumes: `em.emitExpr` (tag, case values), `em.emitBranch` (clause bodies, scoped), `indent`.
- Produces: `func (em *emitter) emitSwitch(sw *ast.SwitchStmt) (string, error)`; `func switchFallthrough(body []ast.Stmt) *ast.BranchStmt`; `emitStmts` handles `*ast.SwitchStmt`; `emitStmt` rejects `*ast.TypeSwitchStmt`.

- [ ] **Step 1: Write the failing tests**

Add to `transpile_test.go`:

```go
func TestModule_Switch(t *testing.T) {
	src := `package m
func Classify(N int) string {
	switch N {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "many"
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	for _, want := range []string{"case N of", "1 -> <<\"one\">>", "2 -> <<\"two\">>", "_ -> <<\"many\">>", "end"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q, got:\n%s", want, r.Erl)
		}
	}
}

func TestModule_SwitchDefaultReorderedLast(t *testing.T) {
	// default is written first in Go; Erlang requires the catch-all last.
	src := `package m
func F(N int) int {
	switch N {
	default:
		return 0
	case 1:
		return 1
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	i1 := strings.Index(r.Erl, "1 -> 1")
	id := strings.Index(r.Erl, "_ -> 0")
	if i1 < 0 || id < 0 || id < i1 {
		t.Errorf("default (_ -> 0) must come after case 1, got:\n%s", r.Erl)
	}
}

func TestModule_SwitchRejections(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"tagless", `package m
func F(N int) int { switch { case N == 1: return 1; default: return 0 } }`, "tagless"},
		{"init", `package m
func F(N int) int { switch M := N; M { case 1: return 1; default: return 0 } }`, "init"},
		{"multi-value", `package m
func F(N int) int { switch N { case 1, 2: return 1; default: return 0 } }`, "multi-value"},
		{"non-literal value", `package m
func F(N int) int { switch N { case N: return 1; default: return 0 } }`, "literal"},
		{"empty clause", `package m
func F(N int) int {
	switch N {
	case 1:
	default:
		return 0
	}
}`, "empty body"},
		{"fallthrough", `package m
func F(N int) int { switch N { case 1: fallthrough; default: return 0 } }`, "fallthrough"},
		{"missing default", `package m
func F(N int) int { switch N { case 1: return 1 } }`, "default"},
		{"type switch", `package m
func F(X interface{}) int { switch X.(type) { case int: return 1; default: return 0 } }`, "type switch"},
	}
	for _, c := range cases {
		_, err := Module(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestModule_SwitchUnreachableAfterRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	switch N {
	case 1:
		return 1
	default:
		return 0
	}
	return N
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("want unreachable rejection, got %v", err)
	}
}

func TestModule_SwitchNonTailRejected(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle(N int) int {
	switch N {
	case 1:
		return 1
	default:
		return 0
	}
	M := otp.Receive().(Msg)
	return M.X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "tail position") {
		t.Fatalf("want non-tail rejection, got %v", err)
	}
}

func TestModule_SwitchSiblingReuse(t *testing.T) {
	src := `package m
func F(N int) int {
	switch N {
	case 1:
		Z := 10
		return Z
	default:
		Z := 20
		return Z
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("want sibling reuse accepted, got %v", err)
	}
	if !strings.Contains(r.Erl, "Z = 10") || !strings.Contains(r.Erl, "Z = 20") {
		t.Errorf("got:\n%s", r.Erl)
	}
}

func TestModule_SwitchClauseOuterCollisionRejected(t *testing.T) {
	src := `package m
func F(Z int) int {
	switch Z {
	case 1:
		Z := 10
		return Z
	default:
		return Z
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want outer-collision rejection, got %v", err)
	}
}
```

Note: the transpiler parses with `go/parser` mode 0 (no type-checking), so sources that a real Go compiler would reject (e.g. `missing return`, unused `M := N`) still parse and reach the emitter — the emitter is what must produce the error.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/transpile/ -run Switch -v`
Expected: FAIL — `*ast.SwitchStmt`/`*ast.TypeSwitchStmt` currently hit `emitStmt`'s `default` → `unsupported statement`.

- [ ] **Step 3: Add `emitSwitch` and the fallthrough helper**

Add these (e.g. right after `emitBranch`):

```go
// emitSwitch emits a tagged expression switch as an Erlang `case Tag of V ->
// clause; … ; _ -> default end`. Only the tagged form is supported: single
// literal case values, a required default (emitted as the catch-all `_` and
// sorted last), each clause body emitted in its own binding scope via
// emitBranch. A type switch is a distinct node handled in emitStmt.
func (em *emitter) emitSwitch(sw *ast.SwitchStmt) (string, error) {
	if sw.Init != nil {
		return "", em.errorf(sw, "switch with an init statement is unsupported (0.3.4+)")
	}
	if sw.Tag == nil {
		return "", em.errorf(sw, "tagless switch is unsupported (0.3.4+); use if")
	}
	tag, err := em.emitExpr(sw.Tag)
	if err != nil {
		return "", err
	}
	var clauses []string // non-default clauses, in source order
	var deflt string
	haveDefault := false
	for _, s := range sw.Body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			return "", em.errorf(s, "unsupported switch clause")
		}
		if ft := switchFallthrough(cc.Body); ft != nil {
			return "", em.errorf(ft, "fallthrough is unsupported")
		}
		if len(cc.Body) == 0 {
			return "", em.errorf(cc, "case clause has no value (empty body)")
		}
		if cc.List == nil { // default clause
			if haveDefault {
				return "", em.errorf(cc, "switch has more than one default")
			}
			haveDefault = true
			deflt, err = em.emitBranch(cc.Body)
			if err != nil {
				return "", err
			}
			continue
		}
		if len(cc.List) != 1 {
			return "", em.errorf(cc, "multi-value case is unsupported (0.3.4+)")
		}
		lit, ok := cc.List[0].(*ast.BasicLit)
		if !ok {
			return "", em.errorf(cc.List[0], "case value must be an int or string literal")
		}
		val, err := em.emitExpr(lit)
		if err != nil {
			return "", err
		}
		body, err := em.emitBranch(cc.Body)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, val+" -> "+body)
	}
	if !haveDefault {
		return "", em.errorf(sw, "switch needs a default clause")
	}
	clauses = append(clauses, "_ -> "+deflt)
	var b strings.Builder
	b.WriteString("case " + tag + " of\n")
	for i, c := range clauses {
		b.WriteString(indent(c))
		if i < len(clauses)-1 {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}
	b.WriteString("end")
	return b.String(), nil
}

// switchFallthrough returns the fallthrough statement in a case body, or nil.
func switchFallthrough(body []ast.Stmt) *ast.BranchStmt {
	for _, s := range body {
		if br, ok := s.(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
			return br
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire `SwitchStmt` into `emitStmts`**

In the `emitStmts` loop, add a `*ast.SwitchStmt` branch immediately after the existing `*ast.IfStmt` branch (before the `*ast.ReturnStmt` check):

```go
		if sw, ok := s.(*ast.SwitchStmt); ok {
			if !isTail {
				return "", em.errorf(sw, "control flow (switch) is only supported in tail position")
			}
			if i != len(list)-1 {
				return "", em.errorf(list[i+1], "unreachable statement after a switch")
			}
			e, err := em.emitSwitch(sw)
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			return strings.Join(parts, ",\n"), nil
		}
```

- [ ] **Step 5: Reject `*ast.TypeSwitchStmt` in `emitStmt`**

In `emitStmt`'s `switch`, add a case before `default`:

```go
	case *ast.TypeSwitchStmt:
		return "", em.errorf(st, "type switch is unsupported (0.3.4+)")
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/pkg/transpile/ -run Switch -v`
Expected: PASS all (`TestModule_Switch`, `SwitchDefaultReorderedLast`, `SwitchRejections`, `SwitchUnreachableAfterRejected`, `SwitchNonTailRejected`, `SwitchSiblingReuse`, `SwitchClauseOuterCollisionRejected`).

- [ ] **Step 7: Run the full transpile suite**

Run: `go test ./internal/pkg/transpile/`
Expected: PASS — existing if/operator/receive tests unaffected.

- [ ] **Step 8: Commit**

```bash
git add internal/pkg/transpile/transpile.go internal/pkg/transpile/transpile_test.go
git commit -s -m "feat(transpile): tagged expression switch -> Erlang case-on-value"
```

---

### Task 2: Real-toolchain rung — a runnable switch

Prove a switch-based function compiles and runs. A classifier fixture transpiles, compiles with `erlc`, and runs to a checked result (`name(2) = "two"`).

**Files:**
- Create: `testdata/switch/classify.go`
- Modify: `internal/pkg/ladder/ladder_integration_test.go` (add the rung; reuses `transpileToErl` + the `erlang.Layout` pattern)

**Interfaces:**
- Consumes (already in `ladder_integration_test.go`): `transpileToErl(t, goPath, dir) string`; `erlang.NewLayout`/`l.Installed()`/`l.Erlc()`/`l.Erl()`; `os`, `os/exec`, `path/filepath`, `strings`, `testing`, `erlang` already imported.
- Produces: green rung `TestRung_Switch` under `-tags integration ./internal/pkg/ladder/`.

- [ ] **Step 1: Write the fixture**

Create `testdata/switch/classify.go`:

```go
// Package classify is a 0.3.3 switch fixture: a tagged expression switch that
// transpiles, compiles with erlc, and runs to a checked result.
package classify

// Name maps a small int to a word via a switch with a default.
func Name(N int) string {
	switch N {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "many"
	}
}
```

- [ ] **Step 2: Write the failing rung**

Append to `internal/pkg/ladder/ladder_integration_test.go`:

```go
// TestRung_Switch transpiles the 0.3.3 classifier fixture (tagged expression
// switch), compiles it with erlc, and RUNS it — proving switch-on-value works
// end to end.
func TestRung_Switch(t *testing.T) {
	home, _ := os.UserHomeDir()
	l := erlang.NewLayout(home, erlang.DefaultVersion)
	if !l.Installed() {
		t.Skip("local Erlang not installed; run erlang provisioning first")
	}
	dir := t.TempDir()
	erl := transpileToErl(t, filepath.FromSlash("../../../testdata/switch/classify.go"), dir)
	if out, err := exec.Command(l.Erlc(), "-o", dir, erl).CombinedOutput(); err != nil {
		t.Fatalf("erlc %s: %v\n%s", erl, err, out)
	}
	out, err := exec.Command(l.Erl(), "-noshell", "-pa", dir,
		"-eval", "io:format(\"~s\", [classify:name(2)]), init:stop().").CombinedOutput()
	if err != nil {
		t.Fatalf("erl run: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "two" {
		t.Fatalf("name(2) = %q, want two", got)
	}
}
```

- [ ] **Step 3: Run the rung — must compile AND run to "two"**

If `erlc` is not installed, provision first: `./bin/wm erlang install`.

Run: `go test -tags integration ./internal/pkg/ladder/ -run TestRung_Switch -v`
Expected: PASS — output `two`. A syntax error from `erlc`, or any output other than `two`, is a real defect in the emitted Erlang — fix the emitter (add a failing unit test), not the rung.

- [ ] **Step 4: Run `go test ./...` (non-integration) to confirm nothing else broke**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add testdata/switch/classify.go internal/pkg/ladder/ladder_integration_test.go
git commit -s -m "test(transpile): real-toolchain rung — switch classifier compiles and runs"
```

---

### Task 3: Verification gate + docs refresh

Run the full gate, confirm the SDK-index unchanged (no `pkg/` change), update `HANDOVER.md`. No new production code.

**Files:**
- Modify: `HANDOVER.md`
- Verify only: `docs/SDK-INDEX.md` (expected unchanged)

- [ ] **Step 1: Build + unit gate**

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
Expected: all rungs green (incl. the new switch rung). If leftover BEAM nodes cause an odd failure, clear with `pkill -9 -x beam.smp; pkill -9 -x epmd` and re-run (see `integration-test-leftover-nodes`).

- [ ] **Step 3: Security sweep (baseline check)**

Run:
```bash
govulncheck ./...
gosec ./...
gitleaks detect
```
Expected: `govulncheck`/`gitleaks` clean; `gosec` unchanged in category from the 0.3.2 baseline (accepted G703 class only; ZERO findings in the transpile package). A NEW unaccepted HIGH/CRITICAL must be triaged.

- [ ] **Step 4: Update the handover**

Update `HANDOVER.md`: 0.3.3 delivered (tagged expression switch → `case`-on-value, per-clause scoping, empty-clause guard, runnable classifier rung), the verification-gate results with the run date, and set the next step (0.3.4: no-default switch / multi-value cases / tagless / type switch, or gen_server callbacks — note the type-switch `v :=` binding is the next `bound-set-integration` context). Move the deferred switch forms into the backlog.

- [ ] **Step 5: Commit**

```bash
git add HANDOVER.md docs/SDK-INDEX.md
git commit -s -m "docs: handover — 0.3.3 switch complete, gate green"
```

---

## Notes for the implementer

- **Release/merge is out of this plan's scope.** These tasks land on a working branch on `origin`. `VERSION` → `0.3.3`, squash to `main`, tag `v0.3.3`, the Copilot gate, pushing to the gated remotes, and the GitHub release are the finishing flow — done after the plan is verified green.
- **The empty-clause guard is load-bearing safety.** An empty case/default body would emit `V -> ;` — invalid Erlang, the exact class the 0.3.2 Copilot gate caught. Keep the `len(cc.Body) == 0` check.
- **Per-clause `emitBranch` is load-bearing correctness** (the `bound-set-integration` invariant): each clause body is scoped so sibling clauses reuse names while outer collisions reject. The sibling-reuse and outer-collision tests guard it.
- **`default` is sorted to the end**, regardless of its Go source position — Erlang requires the catch-all `_` last.
- **A type switch is `*ast.TypeSwitchStmt`, not `*ast.SwitchStmt`** — it never reaches `emitSwitch`; it is rejected in `emitStmt`.
