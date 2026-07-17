// Package transpile turns a valid-Go source file (using pkg/otp) into an
// Erlang module. 0.1.0 targets the echo subset via go/ast.
// ponytail: go/ast pattern-matching for the echo subset; go/types resolution
// is later hardening for arbitrary programs.
package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"sort"
	"strconv"
	"strings"
)

// emitter carries the state needed while emitting Erlang source for a
// single Go file, currently the declared field order of each struct type
// (typeName -> ordered field names) so composite literals can be emitted
// as tagged tuples in the correct order.
type emitter struct {
	structs    map[string][]string
	fset       *token.FileSet
	registered []string
	bound      map[string]bool
	tsAlias    string // active type-switch alias name during clause-body emission; empty otherwise
}

// Result is the full outcome of transpiling one Go file: the Erlang source, the
// module name, the OTP behaviour ("", "gen_server", "supervisor", "application"),
// and the names it registers via otp.StartServer (for the .app resource).
type Result struct {
	Erl        string
	Module     string
	Behaviour  string
	Registered []string
}

// File transpiles src and returns the Erlang source and module name, discarding
// the richer Result fields. Retained for callers that only need the source.
func File(src string) (string, string, error) {
	r, err := Module(src)
	return r.Erl, r.Module, err
}

// AppResource returns the Erlang .app resource body for an OTP application.
// applications is always [kernel, stdlib]; mod is {app, []}.
func AppResource(app, vsn string, modules, registered []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{application, %s,\n", app)
	fmt.Fprintf(&b, " [{description, %q},\n", app)
	fmt.Fprintf(&b, "  {vsn, %q},\n", vsn)
	fmt.Fprintf(&b, "  {modules, [%s]},\n", strings.Join(modules, ", "))
	fmt.Fprintf(&b, "  {registered, [%s]},\n", strings.Join(registered, ", "))
	fmt.Fprintf(&b, "  {applications, [kernel, stdlib]},\n")
	fmt.Fprintf(&b, "  {mod, {%s, []}}]}.\n", app)
	return b.String()
}

// Module parses Go source and emits an Erlang module (Result.Erl), along with
// the module name (the Go package name), the detected OTP behaviour, and the
// names it registers, so callers don't need to re-parse the emitted header.
func Module(src string) (Result, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		return Result{}, err
	}

	structs := map[string][]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			var fields []string
			for _, fld := range st.Fields.List {
				for _, n := range fld.Names {
					if !token.IsExported(n.Name) {
						return Result{}, fmt.Errorf("struct %s field %s is lowercase-leading; Erlang variables must be uppercase",
							ts.Name.Name, n.Name)
					}
					fields = append(fields, n.Name)
				}
			}
			structs[ts.Name.Name] = fields
		}
	}
	em := &emitter{structs: structs, fset: fset}

	var exports []string
	var bodies strings.Builder
	seen := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		params, err := em.paramNames(fn)
		if err != nil {
			return Result{}, err
		}
		name := strings.ToLower(fn.Name.Name)
		if prev, ok := seen[name]; ok {
			return Result{}, fmt.Errorf("functions %s and %s both map to Erlang atom %s (duplicate clause); rename one",
				prev, fn.Name.Name, name)
		}
		seen[name] = fn.Name.Name
		if fn.Name.IsExported() {
			exports = append(exports, fmt.Sprintf("%s/%d", name, len(params)))
		}
		em.bound = map[string]bool{}
		for _, p := range params {
			em.bound[p] = true
		}
		stmts, err := em.emitBody(fn.Body)
		if err != nil {
			return Result{}, err
		}
		// A single-statement body that emits on one line gets a one-line
		// clause (e.g. `start() -> register(...).`). An empty body (which
		// emits the "ok" placeholder) and multi-statement bodies keep the
		// standard indented multi-line form.
		head := name + "(" + strings.Join(params, ", ") + ")"
		if fn.Body != nil && len(fn.Body.List) == 1 && !strings.Contains(stmts, "\n") {
			fmt.Fprintf(&bodies, "\n%s -> %s.\n", head, stmts)
		} else {
			fmt.Fprintf(&bodies, "\n%s ->\n%s.\n", head, indent(stmts))
		}
	}
	// Collect methods by receiver type; a type with an Init method is a
	// gen_server, emitted as behaviour callbacks after the plain functions.
	methods := map[string][]*ast.FuncDecl{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		methods[receiverTypeName(fn)] = append(methods[receiverTypeName(fn)], fn)
	}
	var behaviour string
	var callbacks strings.Builder
	// Iterate method-carrying types in a stable order: Go map iteration is
	// randomized, and emitting callbacks/exports in map order would make the
	// output non-deterministic, breaking the deterministic-compiler guarantee.
	typeNames := make([]string, 0, len(methods))
	for k := range methods {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		ms := methods[typeName]
		if methodNamed(ms, "Start") != nil && methodNamed(ms, "Stop") != nil {
			behaviour = "-behaviour(application).\n"
			exports = append(exports, "start/2", "stop/1")
			start := methodNamed(ms, "Start")
			results, err := returnExprs(start.Body)
			if err != nil {
				return Result{}, em.errorf(start, "Start: %s", err)
			}
			if len(results) != 1 {
				return Result{}, em.errorf(start, "application Start must return the supervisor pid")
			}
			sup, err := em.emitExpr(results[0])
			if err != nil {
				return Result{}, err
			}
			fmt.Fprintf(&callbacks, "\nstart(_Type, _Args) -> %s.\nstop(_State) -> ok.\n", sup)
			continue
		}
		if isSupervisorInit(methodNamed(ms, "Init")) {
			behaviour = "-behaviour(supervisor).\n"
			exports = append(exports, "start_link/0", "init/1")
			children, err := em.supervisorChildren(methodNamed(ms, "Init"))
			if err != nil {
				return Result{}, err
			}
			fmt.Fprintf(&callbacks, "\nstart_link() -> supervisor:start_link({local, %s}, ?MODULE, []).\n", f.Name.Name)
			fmt.Fprintf(&callbacks, "init(_) -> {ok, {{one_for_one, 1, 5}, [%s]}}.\n", strings.Join(children, ", "))
			continue
		}
		initFn := methodNamed(ms, "Init")
		if initFn == nil {
			return Result{}, fmt.Errorf("type %s has methods but no Init; not a recognized gen_server", typeName)
		}
		behaviour = "-behaviour(gen_server).\n"
		exports = append(exports, "init/1")
		results, err := returnExprs(initFn.Body)
		if err != nil {
			return Result{}, em.errorf(initFn, "Init: %s", err)
		}
		state, err := em.emitExpr(results[0])
		if err != nil {
			return Result{}, err
		}
		fmt.Fprintf(&callbacks, "\ninit(_) -> {ok, %s}.\n", state)

		if hc := methodNamed(ms, "HandleCall"); hc != nil {
			exports = append(exports, "handle_call/3")
			// Param -> uppercase Erlang variable (guiding principle: reject lowercase).
			if hc.Type.Params == nil || len(hc.Type.Params.List) != 1 || len(hc.Type.Params.List[0].Names) != 1 {
				return Result{}, em.errorf(hc, "HandleCall must take exactly one parameter")
			}
			param := hc.Type.Params.List[0].Names[0].Name
			if !token.IsExported(param) {
				return Result{}, em.errorf(hc, "HandleCall parameter %s is lowercase-leading; Erlang variables must be uppercase", param)
			}
			// Receiver state head-pattern: {state, F1, F2, ...} binding all fields.
			statePat := []string{strings.ToLower(typeName)}
			statePat = append(statePat, em.structs[typeName]...)
			pattern := "{" + strings.Join(statePat, ", ") + "}"
			// Body: return Reply, NewState -> {reply, Reply, NewState}.
			hcResults, err := returnExprs(hc.Body)
			if err != nil {
				return Result{}, em.errorf(hc, "HandleCall: %s", err)
			}
			if len(hcResults) != 2 {
				return Result{}, em.errorf(hc, "HandleCall must return (reply, state)")
			}
			reply, err := em.emitExpr(hcResults[0])
			if err != nil {
				return Result{}, err
			}
			next, err := em.emitExpr(hcResults[1])
			if err != nil {
				return Result{}, err
			}
			fmt.Fprintf(&callbacks, "handle_call(%s, _From, %s) -> {reply, %s, %s}.\n", param, pattern, reply, next)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "-module(%s).\n", f.Name.Name)
	b.WriteString(behaviour)
	fmt.Fprintf(&b, "-export([%s]).\n", strings.Join(exports, ", "))
	b.WriteString(bodies.String())
	b.WriteString(callbacks.String())
	// behaviourName is derived from the emitted directive so the three branches
	// above don't each have to track it separately.
	behaviourName := ""
	if behaviour != "" {
		behaviourName = strings.TrimSuffix(strings.TrimPrefix(behaviour, "-behaviour("), ").\n")
	}
	return Result{Erl: b.String(), Module: f.Name.Name, Behaviour: behaviourName, Registered: em.registered}, nil
}

// emitBody returns the Erlang expression sequence for a function body.
// Extended in later tasks; empty body -> "ok".
//
// ponytail: single-clause receive only; multi-clause/pattern receive is 0.2.0.
func (em *emitter) emitBody(body *ast.BlockStmt) (string, error) {
	if body == nil || len(body.List) == 0 {
		return "ok", nil
	}
	// Find a `x := otp.Receive().(T)` statement anywhere in the body (not
	// only at position 0): statements before it are emitted as-is, and the
	// remaining statements become the body of the single receive clause.
	for i, s := range body.List {
		as, ok := s.(*ast.AssignStmt)
		if !ok || !em.isReceiveAssign(as) {
			continue
		}
		pre, err := em.emitStmts(body.List[:i], false)
		if err != nil {
			return "", err
		}
		pat, _, err := em.receiveHead(body.List[i:])
		if err != nil {
			return "", err
		}
		inner, err := em.emitStmts(body.List[i+1:], true)
		if err != nil {
			return "", err
		}
		clauseBody := indent(indent(inner)) // two 4-space levels = the receive clause body
		recv := "receive\n" + indent(pat+" ->") + "\n" + clauseBody + "\nend"
		if pre == "" {
			return recv, nil
		}
		return pre + ",\n" + recv, nil
	}
	return em.emitStmts(body.List, true)
}

// isReceiveAssign reports whether as is `x := otp.Receive().(T)`.
func (em *emitter) isReceiveAssign(as *ast.AssignStmt) bool {
	if as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return false
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return false
	}
	c, ok := ta.X.(*ast.CallExpr)
	return ok && isOtpCall(c, "Receive")
}

// emitStmts emits a list of statements as a comma-separated Erlang expression
// sequence. isTail reports whether this list occupies the function's tail
// position (its last statement is the function's value): only then may the
// final statement be a return, emitted as the trailing value. A return in a
// non-tail slice, or before the last statement of a tail slice, is rejected
// (Erlang has no early return; 0.3.2 adds case/if). This distinction matters
// because emitBody splits a receive body into a non-tail `pre` slice and a
// tail clause body.
func (em *emitter) emitStmts(list []ast.Stmt, isTail bool) (string, error) {
	var parts []string
	for i, s := range list {
		if is, ok := s.(*ast.IfStmt); ok {
			if !isTail {
				return "", em.errorf(is, "control flow (if) is only supported in tail position")
			}
			e, err := em.emitIf(is, list[i+1:])
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			return strings.Join(parts, ",\n"), nil // an if consumes the rest of the sequence
		}
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
		if ts, ok := s.(*ast.TypeSwitchStmt); ok {
			if !isTail {
				return "", em.errorf(ts, "control flow (type switch) is only supported in tail position")
			}
			if i != len(list)-1 {
				return "", em.errorf(list[i+1], "unreachable statement after a type switch")
			}
			e, err := em.emitTypeSwitch(ts)
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
			return strings.Join(parts, ",\n"), nil
		}
		if ret, ok := s.(*ast.ReturnStmt); ok {
			if !isTail || i != len(list)-1 {
				return "", em.errorf(ret, "early return is unsupported; put it in an if branch (0.3.2)")
			}
			if len(ret.Results) != 1 {
				return "", em.errorf(ret, "return must yield exactly one value (multi-value return is 0.3.3+)")
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

// emitIf emits an if statement as an Erlang `case Cond of true -> …; false -> …
// end`. The false branch is the explicit else block, or — for a bare if — the
// continuation `cont` (statements following the if). Only if/else and bare-if
// are supported; else-if chains, an init clause, and a bare if with no
// continuation are rejected (0.3.3+).
func (em *emitter) emitIf(is *ast.IfStmt, cont []ast.Stmt) (string, error) {
	if is.Init != nil {
		return "", em.errorf(is, "if with an init statement is unsupported (0.3.3+)")
	}
	if _, ok := is.Else.(*ast.IfStmt); ok {
		return "", em.errorf(is, "else-if chains are unsupported (0.3.3+); use a nested if")
	}
	if len(is.Body.List) == 0 {
		return "", em.errorf(is, "if branch has no value (empty block)")
	}
	cond, err := em.emitExpr(is.Cond)
	if err != nil {
		return "", err
	}
	then, err := em.emitBranch(is.Body.List)
	if err != nil {
		return "", err
	}
	var els string
	switch e := is.Else.(type) {
	case *ast.BlockStmt:
		if len(cont) != 0 {
			return "", em.errorf(cont[0], "unreachable statement after a terminating if/else")
		}
		if len(e.List) == 0 {
			return "", em.errorf(e, "else branch has no value (empty block)")
		}
		els, err = em.emitBranch(e.List)
	case nil:
		if len(cont) == 0 {
			return "", em.errorf(is, "a bare if needs a following value (the case's false branch)")
		}
		if !terminates(is.Body.List) {
			return "", em.errorf(is, "the then-branch of a bare if must end in a return; otherwise it would fall through to the continuation, which a terminal Erlang case clause cannot express")
		}
		els, err = em.emitBranch(cont)
	default:
		return "", em.errorf(is, "unsupported else form")
	}
	if err != nil {
		return "", err
	}
	return "case " + cond + " of\n" +
		indent("true -> "+then) + ";\n" +
		indent("false -> "+els) + "\nend", nil
}

// terminates reports whether a statement list ends in a construct that yields
// the function's value and does not fall through: a return, an if/else whose
// both branches terminate, or an exhaustive switch (a default plus every clause
// terminating). A bare if (no else), or a switch without a default, falls
// through and so does not terminate. Used to reject a bare-if then-branch that
// would fall through to the continuation (Go semantics) but be emitted as a
// terminal case clause (Erlang).
func terminates(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	switch s := list[len(list)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.IfStmt:
		els, ok := s.Else.(*ast.BlockStmt)
		return ok && terminates(s.Body.List) && terminates(els.List)
	case *ast.SwitchStmt:
		hasDefault := false
		for _, cc := range s.Body.List {
			clause, ok := cc.(*ast.CaseClause)
			if !ok {
				return false
			}
			if clause.List == nil {
				hasDefault = true
			}
			if !terminates(clause.Body) {
				return false
			}
		}
		return hasDefault
	case *ast.TypeSwitchStmt:
		// A receive type switch terminates once every clause terminates, with NO
		// default needed: a receive blocks until a message matches and yields
		// that clause's value — it never falls through. A value type switch, like
		// a value case-switch, DOES fall through in Go when no case matches and
		// there is no default, so it terminates only with a default present
		// (emitTypeSwitchValue also requires one).
		hasDefault := false
		for _, cc := range s.Body.List {
			clause, ok := cc.(*ast.CaseClause)
			if !ok || !terminates(clause.Body) {
				return false
			}
			if clause.List == nil {
				hasDefault = true
			}
		}
		return isReceiveTypeSwitch(s) || hasDefault
	default:
		return false
	}
}

// emitBranch emits a case-clause body (an if/else block or a bare-if
// continuation) as a value-yielding Erlang sequence in its own binding scope:
// bound is snapshotted and restored, so a name bound here does not leak to a
// sibling branch (Erlang case clauses are independent scopes), while outer
// bindings stay visible and their collisions stay rejected.
func (em *emitter) emitBranch(list []ast.Stmt) (string, error) {
	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()
	return em.emitStmts(list, true)
}

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

// typeSwitchAssert returns the `X.(type)` assertion of a type switch, whether it
// is aliased (v := X.(type); ts.Assign an *ast.AssignStmt) or tagless (X.(type);
// ts.Assign an *ast.ExprStmt).
func typeSwitchAssert(ts *ast.TypeSwitchStmt) (*ast.TypeAssertExpr, bool) {
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if a.Tok == token.DEFINE && len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				return ta, true
			}
		}
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok {
			return ta, true
		}
	}
	return nil, false
}

// emitTypeSwitch dispatches a tail-position type switch on its operand. Both
// the alias-binding form (`switch V := X.(type)`) and the tagless form
// (`switch X.(type)`) are supported; an init statement is rejected. em.tsAlias
// is set for the whole emission (empty for tagless) so V.Field resolves in
// clause bodies and a bare V is rejected. otp.Receive() as the operand lowers
// to a multi-clause `receive`; any other value lowers to a `case X of … end`.
func (em *emitter) emitTypeSwitch(ts *ast.TypeSwitchStmt) (string, error) {
	if ts.Init != nil {
		return "", em.errorf(ts, "type switch with an init statement is unsupported (0.3.8+)")
	}
	alias := ""
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return "", em.errorf(ts, "unsupported type switch binding")
		}
		id, ok := a.Lhs[0].(*ast.Ident)
		if !ok {
			return "", em.errorf(ts, "type switch alias must be a single identifier")
		}
		alias = id.Name
	case *ast.ExprStmt:
		// tagless: no alias
	default:
		return "", em.errorf(ts, "unsupported type switch form")
	}
	old := em.tsAlias
	em.tsAlias = alias
	defer func() { em.tsAlias = old }()
	if isReceiveTypeSwitch(ts) {
		return em.emitTypeSwitchReceive(ts)
	}
	return em.emitTypeSwitchValue(ts)
}

// emitTypeSwitchValue lowers `switch V := X.(type)` over a plain value X to an
// Erlang `case X of {tag, Field…} -> body; … end`. Struct-typed cases only,
// reusing emitTypeSwitchClauses. A `default:` is REQUIRED: unlike a receive
// (which blocks until a message matches), a value type switch with no matching
// case falls through in Go — ordinary control flow — which a total Erlang
// `case` cannot express (it would raise case_clause instead). So a default-less
// value switch is rejected rather than silently mis-transpiled; the default
// becomes the trailing `_ ->` catch-all.
func (em *emitter) emitTypeSwitchValue(ts *ast.TypeSwitchStmt) (string, error) {
	ta, _ := typeSwitchAssert(ts)
	operand, err := em.emitExpr(ta.X)
	if err != nil {
		return "", err
	}
	clauses, haveDefault, err := em.emitTypeSwitchClauses(ts)
	if err != nil {
		return "", err
	}
	if !haveDefault {
		return "", em.errorf(ts, "a plain-value type switch requires a default clause; without it a value matching no case falls through in Go, which a total Erlang `case` cannot express")
	}
	return wrapClauses("case "+operand+" of", clauses), nil
}

// emitTypeSwitchReceive wraps the shared clauses in a multi-clause `receive`.
// Precondition: em.tsAlias is set and the operand is otp.Receive() (guaranteed
// by emitTypeSwitch, which dispatches here only when isReceiveTypeSwitch holds).
// A default is optional here: without it the receive is selective (it blocks on
// a non-matching message rather than falling through).
func (em *emitter) emitTypeSwitchReceive(ts *ast.TypeSwitchStmt) (string, error) {
	clauses, _, err := em.emitTypeSwitchClauses(ts)
	if err != nil {
		return "", err
	}
	return wrapClauses("receive", clauses), nil
}

// wrapClauses assembles an Erlang clause block: `<header>\n  C1;\n  C2\nend`,
// each clause indented and semicolon-separated. Shared by the receive
// (header "receive") and value (header "case X of") type-switch wrappers.
func wrapClauses(header string, clauses []string) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for i, c := range clauses {
		b.WriteString(indent(c))
		if i < len(clauses)-1 {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}
	b.WriteString("end")
	return b.String()
}

// emitTypeSwitchClauses emits the ordered Erlang clauses for a type switch's
// case bodies: one "Pattern -> Body" per listed type (a multi-type case, case
// A, B:, expands to one clause per type sharing the body — emitCaseClause),
// plus a trailing default clause when a default: is present — "_ -> Body"
// normally, or "V -> Body" (the alias itself, a total catch-all binding) when
// the default body uses the alias as a bare value, since Go's `default` binds
// it to the whole original value. Two cases lowering to the same message tag
// are rejected (the second would be unreachable in Erlang). em.tsAlias must be
// set by the caller so v.Field access resolves and a bare alias is rejected.
// Operand- and wrapper-agnostic: shared by the receive (receive … end) and
// value (case X of … end) paths. Also returns whether a default: was present,
// so the value path can require one (a value switch falls through in Go)
// while the receive path leaves it optional.
func (em *emitter) emitTypeSwitchClauses(ts *ast.TypeSwitchStmt) ([]string, bool, error) {
	var clauses []string
	var deflt string
	defaultPattern := "_"
	haveDefault := false
	seenTag := map[string]bool{}
	for _, s := range ts.Body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			return nil, false, em.errorf(s, "unsupported type-switch clause")
		}
		if len(cc.Body) == 0 {
			return nil, false, em.errorf(cc, "case clause has no value (empty body)")
		}
		if cc.List == nil { // default
			if haveDefault {
				return nil, false, em.errorf(cc, "type switch has more than one default")
			}
			haveDefault = true
			// Go's `default` binds the alias to the WHOLE original value; a bare
			// V in the default body must lower to a bound catch-all `V -> Body`
			// (total, matches anything), not the unbound `_ -> Body` used when
			// the body never references V. registerAlias mutates em.bound before
			// emitBranch's own snapshot/restore runs, so the pre-registration
			// snapshot is taken here and restored after emitting the body — the
			// binding must not leak past the default clause.
			if bodyUsesBareAlias(cc.Body, em.tsAlias) {
				snap := maps.Clone(em.bound)
				if err := em.registerAlias(cc); err != nil {
					return nil, false, err
				}
				body, err := em.emitBranch(cc.Body)
				if err != nil {
					return nil, false, err
				}
				em.bound = snap
				deflt = body
				defaultPattern = em.tsAlias
				continue
			}
			body, err := em.emitBranch(cc.Body)
			if err != nil {
				return nil, false, err
			}
			deflt = body
			continue
		}
		clausesForCase, err := em.emitCaseClause(cc, seenTag)
		if err != nil {
			return nil, false, err
		}
		clauses = append(clauses, clausesForCase...)
	}
	if haveDefault {
		clauses = append(clauses, defaultPattern+" -> "+deflt)
	}
	return clauses, haveDefault, nil
}

// primitiveGuard maps a supported non-struct type-switch case type to its Erlang
// type guard. The four guards are pairwise disjoint (and disjoint from
// atom-tagged struct tuples), so Erlang's runtime first-match order coincides
// with Go's static per-case type exclusivity.
var primitiveGuard = map[string]string{
	"int":     "is_integer",
	"string":  "is_binary",
	"bool":    "is_boolean",
	"float64": "is_float",
}

// tsThrowaway is the Erlang variable a primitive case guards when the value is
// not bound to the alias (tagless, or a multi-type/unused body). The underscore
// prefix suppresses the "unused variable" warning; a bare _ is illegal in a guard.
const tsThrowaway = "_X"

// structWildcardPattern returns the Erlang tuple pattern for a declared struct
// with every field wildcarded ({ping, _, _}) — matches the tag and arity without
// binding anything. Used where fields cannot be bound: multi-type and tagless
// cases. Precondition: typeName is a declared struct (caseTypeName checked it).
func (em *emitter) structWildcardPattern(typeName string) string {
	parts := []string{strings.ToLower(typeName)}
	for range em.structs[typeName] {
		parts = append(parts, "_")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// bodyUsesAliasField reports whether the body accesses a field of the alias
// (alias.Field). A multi-type case forbids it — Go keeps v at the interface type
// there, and emitting the field name would reference an unbound (or unrelated,
// silently wrong) Erlang variable.
func bodyUsesAliasField(body []ast.Stmt, alias string) bool {
	found := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
					found = true
				}
			}
			return true
		})
	}
	return found
}

// bodyCopiesAlias reports whether the body binds a new variable derived from the
// alias — its bare form (W := V) or any expression referencing it (X := f(V)). In
// a multi-type case this must be rejected: such a binding lets a later X.Field
// slip past bodyUsesAliasField and lower (via emitExpr) to a bare field name that
// is unbound or silently resolves to an unrelated outer binding — the same
// mis-transpile bodyUsesAliasField guards against. The RHS is scanned
// transitively (not just a top-level ident), so threading the alias through a
// call before binding is caught too. Banning any alias-derived binding leaves the
// alias usable only as a whole value passed or returned directly, for which
// v.Field is the sole field-access path and is already rejected.
func bodyCopiesAlias(body []ast.Stmt, alias string) bool {
	found := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range as.Rhs {
				ast.Inspect(rhs, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok && id.Name == alias {
						found = true
					}
					return true
				})
			}
			return true
		})
	}
	return found
}

// emitCaseClause emits the Erlang clauses for one non-default type-switch case: a
// single-type case yields one clause, a multi-type case (case A, B:) yields one
// clause per listed type, all sharing the (duplicated) body. Fields are bound only
// for a single-type, aliased struct case (structPattern); a multi-type case or a
// tagless switch (no alias) wildcards struct fields and never binds fields (with
// no alias in scope there is nothing to bind v.Field to). The alias binds the
// whole value (V = {tag,_} / V when guard(V)) when the body uses bare V; otherwise
// struct fields wildcard and a primitive guards the tsThrowaway variable. Each
// listed type is deduped on its runtime discriminant; em.bound is
// snapshotted/restored around the clause.
func (em *emitter) emitCaseClause(cc *ast.CaseClause, seenTag map[string]bool) ([]string, error) {
	isMulti := len(cc.List) > 1

	if isMulti && bodyUsesAliasField(cc.Body, em.tsAlias) {
		return nil, em.errorf(cc, "field access is not allowed in a multi-type case; v keeps the interface type (bare v is allowed, v.Field is not)")
	}
	if isMulti && bodyCopiesAlias(cc.Body, em.tsAlias) {
		return nil, em.errorf(cc, "the matched value cannot be copied to another variable in a multi-type case; v keeps the interface type — use v directly as a whole value")
	}

	// Classify + dedup every listed type before emitting.
	type caseType struct {
		name, guard string
		at          ast.Expr
	}
	var types []caseType
	for _, e := range cc.List {
		name, guard, err := em.caseTypeName(e)
		if err != nil {
			return nil, err
		}
		key := "t:" + strings.ToLower(name)
		if guard != "" {
			key = "g:" + guard
		}
		if seenTag[key] {
			if guard != "" {
				return nil, em.errorf(e, "type switch has two cases of type %s; the second clause would be unreachable in Erlang", name)
			}
			return nil, em.errorf(e, "type switch has two cases with the same message tag %q; the second clause would be unreachable in Erlang (e.g. Ping and *Ping, or names differing only in case)", strings.ToLower(name))
		}
		seenTag[key] = true
		types = append(types, caseType{name, guard, e})
	}

	bindFields := !isMulti && em.tsAlias != ""
	bindAlias := em.tsAlias != "" && bodyUsesBareAlias(cc.Body, em.tsAlias)

	snap := maps.Clone(em.bound)
	defer func() { em.bound = snap }()

	if bindAlias {
		if err := em.registerAlias(cc); err != nil {
			return nil, err
		}
	}

	// Build one pattern per listed type.
	var pats []string
	for _, ct := range types {
		if ct.guard != "" { // primitive
			v := tsThrowaway
			if bindAlias {
				v = em.tsAlias
			}
			pats = append(pats, v+" when "+ct.guard+"("+v+")")
			continue
		}
		var pat string
		if bindFields {
			p, err := em.structPattern(ct.name, ct.at)
			if err != nil {
				return nil, err
			}
			pat = p
		} else {
			pat = em.structWildcardPattern(ct.name)
		}
		if bindAlias {
			pat = em.tsAlias + " = " + pat
		}
		pats = append(pats, pat)
	}

	body, err := em.emitStmts(cc.Body, true)
	if err != nil {
		return nil, err
	}

	clauses := make([]string, 0, len(pats))
	for _, p := range pats {
		clauses = append(clauses, p+" -> "+body)
	}
	return clauses, nil
}

// caseTypeName classifies a single type-switch case expression, accepting both
// `T` and `*T` (Erlang has no pointers, so the star is meaningless). For a
// declared struct it returns (name, "", nil); for a supported primitive it
// returns (name, guard, nil); anything else is rejected.
func (em *emitter) caseTypeName(e ast.Expr) (name, guard string, err error) {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", "", em.errorf(e, "type switch case must name a type")
	}
	if _, ok := em.structs[id.Name]; ok {
		return id.Name, "", nil
	}
	if g, ok := primitiveGuard[id.Name]; ok {
		return id.Name, g, nil
	}
	return "", "", em.errorf(e, "unsupported type-switch case type %s; only declared structs and int/string/bool/float64 are supported (0.3.7+)", id.Name)
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

// structPattern returns the Erlang tuple pattern for a declared struct type
// (atom = lowercased type name, each declared field bound to its capitalized
// field name) and registers each field name as a bound Erlang variable. It
// errors if typeName is not a declared struct, or if a field name collides with
// an already-bound name — in Erlang an already-bound pattern variable is an
// equality match, not a fresh binding, so emitting it would silently change the
// semantics.
func (em *emitter) structPattern(typeName string, at ast.Node) (string, error) {
	fields, ok := em.structs[typeName]
	if !ok {
		return "", em.errorf(at, "unknown struct type %s", typeName)
	}
	parts := []string{strings.ToLower(typeName)}
	for _, fld := range fields {
		if em.bound[fld] {
			return "", em.errorf(at, "receive pattern field %s collides with an already-bound name; Erlang would treat it as an equality match, not a fresh binding — rename one", fld)
		}
		em.bound[fld] = true
		parts = append(parts, fld)
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}

// registerAlias binds the type-switch alias as an Erlang variable in the current
// (snapshotted) bound scope. It rejects a lowercase alias (which would emit an
// Erlang atom, not a variable) and a collision with an already-bound name (a
// parameter, a prior :=, or a struct field — in Erlang a rebind is an equality
// match, not a fresh binding). Call it only when the clause actually uses the
// alias as an Erlang variable.
func (em *emitter) registerAlias(at ast.Node) error {
	a := em.tsAlias
	if !token.IsExported(a) {
		return em.errorf(at, "type-switch alias %s is lowercase-leading; Erlang variables must be uppercase", a)
	}
	if em.bound[a] {
		return em.errorf(at, "type-switch alias %s collides with an already-bound name (a parameter, a prior :=, or a struct field); Erlang would treat it as an equality match, not a fresh binding — rename one", a)
	}
	em.bound[a] = true
	return nil
}

// bodyUsesBareAlias reports whether any statement in body references the
// type-switch alias as a whole value (a bare identifier) rather than only via
// field access (alias.Field). It drives whether a struct clause binds the whole
// tuple (V = {tag, Fields}); emitExpr lowers exactly the same bare references to
// the Erlang variable, so detection and emission stay in lockstep.
func bodyUsesBareAlias(body []ast.Stmt, alias string) bool {
	// Two passes: a *ast.SelectorExpr's .Sel is always a field name (never a
	// bare-variable use of the alias), and its .X, when it is the alias ident,
	// is the field-access receiver (not a bare use). Both idents are collected
	// into a skip-set first, so an unrelated selector's field named like the
	// alias (e.g. W.V when the alias is V) is never misdetected as a bare use.
	skip := map[*ast.Ident]bool{}
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if shadowsAlias(n, alias) {
				return false // inner switch rebinds the alias; its body is the inner's scope
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				skip[sel.Sel] = true // field name, never a bare use
				if id, ok := sel.X.(*ast.Ident); ok {
					skip[id] = true // receiver of field access, not a bare use
				}
			}
			return true
		})
	}
	used := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if shadowsAlias(n, alias) {
				return false
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == alias && !skip[id] {
				used = true
			}
			return true
		})
	}
	return used
}

// shadowsAlias reports whether n is a nested type switch that rebinds `alias`
// (binds the same name), so a bare alias reference inside its body belongs to
// the inner switch, not the one being scanned — matching how emitExpr resolves
// against the innermost em.tsAlias. Residual: a nested same-alias switch whose
// *operand* references the outer alias (e.g. `switch V := V.Data.(type)`) still
// reads that occurrence as the inner scope's own use — but since the inner
// switch's operand runs before its own alias exists in Erlang, a genuine
// mismatch there surfaces as a loud unbound-variable error, never silently.
func shadowsAlias(n ast.Node, alias string) bool {
	ts, ok := n.(*ast.TypeSwitchStmt)
	if !ok {
		return false
	}
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 {
		return false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	return ok && id.Name == alias
}

// receiveHead recognizes a leading `x := otp.Receive().(T)` statement, returns
// the Erlang tuple pattern for T (atom = lowercased type name, each field bound
// to its capitalized field name) plus the remaining statements, and registers
// each field name as a bound Erlang variable. A field name that collides with
// an already-bound name (a parameter, a prior `:=`, or a prior receive field)
// is rejected: in Erlang an already-bound pattern variable is an equality match,
// not a fresh binding, so emitting it would silently change the semantics.
func (em *emitter) receiveHead(list []ast.Stmt) (pattern string, rest []ast.Stmt, err error) {
	as, ok := list[0].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return "", nil, em.errorf(list[0], "internal: expected a receive-assign statement")
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return "", nil, em.errorf(as, "internal: expected a receive type assertion")
	}
	call, ok := ta.X.(*ast.CallExpr)
	if !ok || !isOtpCall(call, "Receive") {
		return "", nil, em.errorf(as, "internal: expected otp.Receive")
	}
	typ, ok := ta.Type.(*ast.Ident)
	if !ok {
		return "", nil, em.errorf(as, "otp.Receive type assertion must name a struct type")
	}
	pat, err := em.structPattern(typ.Name, as)
	if err != nil {
		return "", nil, err
	}
	return pat, list[1:], nil
}

// isOtpCall reports whether c is a call to otp.<name>.
func isOtpCall(c *ast.CallExpr, name string) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return otpPkgIdent(sel.X) && sel.Sel.Name == name
}

// otpPkgIdent reports whether x is the bare package identifier `otp`.
func otpPkgIdent(x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Name == "otp"
}

// isReceiveTypeSwitch reports whether ts is `v := otp.Receive().(type)` — a
// type switch whose operand is otp.Receive(). It selects the receive lowering
// (a multi-clause receive, default optional) over the plain-value lowering
// (`case X of`, default required); it also lets terminates() treat a
// default-less receive switch as terminating while a value switch is not.
func isReceiveTypeSwitch(ts *ast.TypeSwitchStmt) bool {
	ta, ok := typeSwitchAssert(ts)
	if !ok || ta.Type != nil { // .(type), not .(T)
		return false
	}
	call, ok := ta.X.(*ast.CallExpr)
	return ok && isOtpCall(call, "Receive")
}

func (em *emitter) emitStmt(s ast.Stmt) (string, error) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		return em.emitExpr(st.X)
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
	default:
		return "", em.errorf(s, "unsupported statement: %T", s)
	}
}

// binOp maps Go binary operators to their Erlang spelling. Equality is exact
// (=:= / =/=), matching Go's non-coercing == on ints and atoms.
var binOp = map[token.Token]string{
	token.ADD: "+", token.SUB: "-", token.MUL: "*",
	token.QUO: "div", token.REM: "rem",
	token.EQL: "=:=", token.NEQ: "=/=",
	token.LSS: "<", token.GTR: ">", token.LEQ: "=<", token.GEQ: ">=",
	token.LAND: "andalso", token.LOR: "orelse",
}

// unparen strips parenthesis layers from e.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// emitOperand emits e as an operand of a binary/unary operator, wrapping it in
// parentheses when (ignoring existing parens) it is itself a binary expression,
// so Go's grouping survives regardless of Erlang's operator precedence. A single
// operator stays bare (X + Y, not (X + Y)).
func (em *emitter) emitOperand(e ast.Expr) (string, error) {
	s, err := em.emitExpr(e)
	if err != nil {
		return "", err
	}
	if _, ok := unparen(e).(*ast.BinaryExpr); ok {
		return "(" + s + ")", nil
	}
	return s, nil
}

func (em *emitter) emitExpr(e ast.Expr) (string, error) {
	switch ex := e.(type) {
	case *ast.BasicLit:
		switch ex.Kind {
		case token.STRING:
			return "<<" + ex.Value + ">>", nil // ex.Value keeps the quotes
		case token.INT:
			// Normalize every Go integer literal (decimal, 0-octal, 0o/0x/0b,
			// digit separators) to a plain decimal Erlang integer. Emitting the
			// Go spelling verbatim is wrong (Erlang reads 0777 as 777) or invalid
			// (0x1F). base 0 lets ParseInt auto-detect Go's prefixes/underscores.
			n, err := strconv.ParseInt(ex.Value, 0, 64)
			if err != nil {
				return "", em.errorf(ex, "unsupported integer literal %s: %v", ex.Value, err)
			}
			return strconv.FormatInt(n, 10), nil
		}
		return "", em.errorf(ex, "unsupported literal: %s", ex.Value)
	case *ast.BinaryExpr:
		op, ok := binOp[ex.Op]
		if !ok {
			return "", em.errorf(ex, "unsupported binary operator %s (0.3.3+)", ex.Op)
		}
		l, err := em.emitOperand(ex.X)
		if err != nil {
			return "", err
		}
		r, err := em.emitOperand(ex.Y)
		if err != nil {
			return "", err
		}
		return l + " " + op + " " + r, nil
	case *ast.UnaryExpr:
		if ex.Op != token.NOT {
			return "", em.errorf(ex, "unsupported unary operator %s", ex.Op)
		}
		x, err := em.emitOperand(ex.X)
		if err != nil {
			return "", err
		}
		return "not " + x, nil
	case *ast.ParenExpr:
		return em.emitExpr(ex.X)
	case *ast.CallExpr:
		return em.emitCall(ex)
	case *ast.CompositeLit:
		typ, ok := ex.Type.(*ast.Ident)
		if !ok {
			return "", em.errorf(ex, "unsupported composite literal")
		}
		order := em.structs[typ.Name]
		byField := map[string]string{}
		for _, elt := range ex.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return "", em.errorf(ex, "struct literal needs field: value")
			}
			key := kv.Key.(*ast.Ident).Name
			v, err := em.emitExpr(kv.Value)
			if err != nil {
				return "", err
			}
			byField[key] = v
		}
		parts := []string{strings.ToLower(typ.Name)}
		for _, fn := range order {
			v, ok := byField[fn]
			if !ok {
				return "", em.errorf(ex, "struct literal for %s omits field %s", typ.Name, fn)
			}
			parts = append(parts, v)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	case *ast.SelectorExpr:
		// req.From -> the bound variable From (capitalized field name)
		if _, ok := ex.X.(*ast.Ident); ok {
			return ex.Sel.Name, nil // field name is already Erlang-variable-cased (From, Text)
		}
		return "", em.errorf(ex, "unsupported selector")
	case *ast.Ident:
		// The active type-switch alias (e.g. `V` in `switch V := otp.Receive().(type)`)
		// is emitted as its Erlang variable when the clause binds it (whole-alias:
		// V = {tag, Fields}, see emitCaseClause/bodyUsesBareAlias); an uppercase bare
		// alias falls through to the token.IsExported branch below like any other
		// bound variable, a lowercase one is rejected there too.
		//
		// A pre-bound variable reference (e.g. From/Text bound in a receive
		// pattern) must be an uppercase-leading Erlang variable. A lowercase
		// ident would emit an Erlang atom, not a variable — silently wrong —
		// so reject it, consistent with the A2 field-casing guard.
		if !token.IsExported(ex.Name) {
			return "", em.errorf(ex, "bare identifier %s is lowercase-leading; Erlang variables must be uppercase", ex.Name)
		}
		return ex.Name, nil
	case *ast.TypeAssertExpr:
		// x.(T) outside a receive: Erlang is dynamically typed, so the
		// assertion is Go-only — emit the inner expression.
		return em.emitExpr(ex.X)
	default:
		return "", em.errorf(e, "unsupported expression: %T", e)
	}
}

// emitArgs emits each call argument as an Erlang expression, preserving order.
func (em *emitter) emitArgs(exprs []ast.Expr) ([]string, error) {
	args := make([]string, len(exprs))
	for i, a := range exprs {
		s, err := em.emitExpr(a)
		if err != nil {
			return nil, err
		}
		args[i] = s
	}
	return args, nil
}

func (em *emitter) emitCall(c *ast.CallExpr) (string, error) {
	// bare self-call: Serve()
	if id, ok := c.Fun.(*ast.Ident); ok {
		args, err := em.emitArgs(c.Args)
		if err != nil {
			return "", err
		}
		return strings.ToLower(id.Name) + "(" + strings.Join(args, ", ") + ")", nil
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", em.errorf(c, "unsupported call target: %T", c.Fun)
	}
	if !otpPkgIdent(sel.X) {
		return "", em.errorf(c, "unsupported call: %s", sel.Sel.Name)
	}
	// otp.Spawn takes a bare function identifier, not an expression, so it
	// is handled before the general arg-emission loop below (emitExpr has
	// no case for a bare *ast.Ident).
	if sel.Sel.Name == "Spawn" {
		id, ok := c.Args[0].(*ast.Ident)
		if !ok {
			return "", em.errorf(c, "otp.Spawn requires a function identifier argument")
		}
		return fmt.Sprintf("spawn(fun ?MODULE:%s/0)", strings.ToLower(id.Name)), nil
	}
	// otp.StartServer("echo", State{}) — the second arg is a type marker (which
	// gen_server type carries the callbacks); the current module IS the
	// gen_server (?MODULE), so it is not emitted as a runtime value.
	if sel.Sel.Name == "StartSupervisor" {
		if len(c.Args) != 1 {
			return "", em.errorf(c, "otp.StartSupervisor takes one supervisor value")
		}
		lit, ok := c.Args[0].(*ast.CompositeLit)
		if !ok {
			return "", em.errorf(c, "otp.StartSupervisor requires a supervisor value, e.g. echosup.Sup{}")
		}
		selT, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return "", em.errorf(c, "otp.StartSupervisor argument must be pkg.Type{}")
		}
		pkg, ok := selT.X.(*ast.Ident)
		if !ok {
			return "", em.errorf(c, "otp.StartSupervisor argument must be pkg.Type{}")
		}
		return pkg.Name + ":start_link()", nil
	}
	if sel.Sel.Name == "StartServer" {
		name, err := em.emitExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		em.registered = append(em.registered, unquoteAtom(name))
		return fmt.Sprintf("gen_server:start_link({local, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
	}
	if sel.Sel.Name == "StartServerGlobal" {
		name, err := em.emitExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("gen_server:start_link({global, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
	}
	// Guard arity before indexing args below: Module only parses (no
	// type-checking), so a wrong-arity marker call like otp.Call("x") reaches
	// here and must yield a positioned error, not an index-out-of-range panic.
	arity := map[string]int{
		"Send": 2, "Register": 2, "Whereis": 1, "RegisterGlobal": 2,
		"WhereisGlobal": 1, "Call": 2, "CallGlobal": 2, "Self": 0, "Print": 1,
	}
	if n, ok := arity[sel.Sel.Name]; ok && len(c.Args) != n {
		return "", em.errorf(c, "otp.%s expects %d argument(s), got %d", sel.Sel.Name, n, len(c.Args))
	}
	args, err := em.emitArgs(c.Args)
	if err != nil {
		return "", err
	}
	switch sel.Sel.Name {
	case "Send":
		return args[0] + " ! " + args[1], nil
	case "Register":
		return fmt.Sprintf("register(%s, %s)", unquoteAtom(args[0]), args[1]), nil
	case "Whereis":
		return fmt.Sprintf("whereis(%s)", unquoteAtom(args[0])), nil
	case "RegisterGlobal":
		return fmt.Sprintf("global:register_name(%s, %s)", unquoteAtom(args[0]), args[1]), nil
	case "WhereisGlobal":
		return fmt.Sprintf("global:whereis_name(%s)", unquoteAtom(args[0])), nil
	case "Call":
		return fmt.Sprintf("gen_server:call(%s, %s)", unquoteAtom(args[0]), args[1]), nil
	case "CallGlobal":
		return fmt.Sprintf("gen_server:call({global, %s}, %s)", unquoteAtom(args[0]), args[1]), nil
	case "Self":
		return "self()", nil
	case "Print":
		return fmt.Sprintf("io:format(\"~s~n\", [%s])", args[0]), nil
	default:
		return "", em.errorf(c, "unsupported otp call: %s", sel.Sel.Name)
	}
}

// errorf formats an error prefixed with n's source position (src.go:line:col).
func (em *emitter) errorf(n ast.Node, format string, a ...any) error {
	return fmt.Errorf("%s: %s", em.fset.Position(n.Pos()), fmt.Sprintf(format, a...))
}

// unquoteAtom turns <<"echo">> back into the bare atom echo (for register/whereis names).
func unquoteAtom(s string) string {
	s = strings.TrimPrefix(s, "<<\"")
	s = strings.TrimSuffix(s, "\">>")
	return s
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

// paramNames returns the ordered parameter names of fn, flattening grouped
// declarations (X, Y int -> [X, Y]). Each name becomes an Erlang variable, so a
// lowercase-leading name is rejected (never auto-capitalized).
func (em *emitter) paramNames(fn *ast.FuncDecl) ([]string, error) {
	var names []string
	if fn.Type.Params == nil {
		return names, nil
	}
	for _, fld := range fn.Type.Params.List {
		if len(fld.Names) == 0 {
			return nil, em.errorf(fld, "unnamed parameter is unsupported; every parameter needs an uppercase name")
		}
		for _, n := range fld.Names {
			if !token.IsExported(n.Name) {
				return nil, em.errorf(n, "parameter %s is lowercase-leading; Erlang variables must be uppercase", n.Name)
			}
			names = append(names, n.Name)
		}
	}
	return names, nil
}

// receiverTypeName returns the name of fn's receiver type (value or pointer),
// or "" if fn has no receiver.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// isSupervisorInit reports whether fn is an `Init() []otp.Child` method, which
// marks a supervisor (as opposed to a gen_server's `Init() State`).
func isSupervisorInit(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	arr, ok := fn.Type.Results.List[0].Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	sel, ok := arr.Elt.(*ast.SelectorExpr)
	return ok && otpPkgIdent(sel.X) && sel.Sel.Name == "Child"
}

// supervisorChildren emits one Erlang child spec string per otp.Child in the
// supervisor Init's returned []otp.Child literal. Each child's Start is a
// package-qualified function value (pkg.Fn) mapped to the MFA {pkg, fn, []}.
func (em *emitter) supervisorChildren(fn *ast.FuncDecl) ([]string, error) {
	results, err := returnExprs(fn.Body)
	if err != nil {
		return nil, em.errorf(fn, "Init: %s", err)
	}
	if len(results) != 1 {
		return nil, em.errorf(fn, "supervisor Init must return one []otp.Child")
	}
	lit, ok := results[0].(*ast.CompositeLit)
	if !ok {
		return nil, em.errorf(fn, "supervisor Init must return an []otp.Child literal")
	}
	var specs []string
	for _, elt := range lit.Elts {
		child, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, em.errorf(elt, "supervisor child must be an otp.Child literal")
		}
		var id, mod, function string
		for _, e := range child.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				return nil, em.errorf(e, "otp.Child needs field: value")
			}
			switch kv.Key.(*ast.Ident).Name {
			case "ID":
				bl, ok := kv.Value.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return nil, em.errorf(kv.Value, "otp.Child ID must be a string literal")
				}
				id = strings.Trim(bl.Value, `"`)
			case "Start":
				sel, ok := kv.Value.(*ast.SelectorExpr)
				if !ok {
					return nil, em.errorf(kv.Value, "otp.Child Start must be a package-qualified function, e.g. echoserver.Start")
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return nil, em.errorf(kv.Value, "otp.Child Start must be pkg.Func")
				}
				mod = pkg.Name
				function = strings.ToLower(sel.Sel.Name)
			default:
				return nil, em.errorf(kv.Key, "unsupported otp.Child field %s", kv.Key.(*ast.Ident).Name)
			}
		}
		if id == "" || mod == "" {
			return nil, em.errorf(child, "otp.Child needs both ID and Start")
		}
		specs = append(specs, fmt.Sprintf("{%s, {%s, %s, []}, permanent, 5000, worker, [%s]}", id, mod, function, mod))
	}
	return specs, nil
}

// methodNamed returns the method named name from ms, or nil.
func methodNamed(ms []*ast.FuncDecl, name string) *ast.FuncDecl {
	for _, m := range ms {
		if m.Name.Name == name {
			return m
		}
	}
	return nil
}

// returnExprs returns the expressions of the single return statement in body,
// or an error if the body is not exactly one return statement.
func returnExprs(body *ast.BlockStmt) ([]ast.Expr, error) {
	if body == nil || len(body.List) != 1 {
		return nil, fmt.Errorf("callback body must be a single return statement")
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok {
		return nil, fmt.Errorf("callback body must be a return statement")
	}
	return ret.Results, nil
}
