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
	"sort"
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
		if ret, ok := s.(*ast.ReturnStmt); ok {
			if !isTail || i != len(list)-1 {
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
	parts := []string{strings.ToLower(typ.Name)}
	for _, fld := range em.structs[typ.Name] {
		if em.bound[fld] {
			return "", nil, em.errorf(as, "receive pattern field %s collides with an already-bound name; Erlang would treat it as an equality match, not a fresh binding — rename one", fld)
		}
		em.bound[fld] = true
		parts = append(parts, fld) // field name is the freshly bound Erlang variable
	}
	return "{" + strings.Join(parts, ", ") + "}", list[1:], nil
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

func (em *emitter) emitExpr(e ast.Expr) (string, error) {
	switch ex := e.(type) {
	case *ast.BasicLit:
		switch ex.Kind {
		case token.STRING:
			return "<<" + ex.Value + ">>", nil // ex.Value keeps the quotes
		case token.INT:
			return ex.Value, nil
		}
		return "", em.errorf(ex, "unsupported literal: %s", ex.Value)
	case *ast.BinaryExpr:
		if ex.Op != token.ADD {
			return "", em.errorf(ex, "unsupported binary operator %s (only + in the gen_server subset)", ex.Op)
		}
		l, err := em.emitExpr(ex.X)
		if err != nil {
			return "", err
		}
		r, err := em.emitExpr(ex.Y)
		if err != nil {
			return "", err
		}
		return l + " + " + r, nil
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
