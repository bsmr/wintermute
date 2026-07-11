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
	"strings"
)

// emitter carries the state needed while emitting Erlang source for a
// single Go file, currently the declared field order of each struct type
// (typeName -> ordered field names) so composite literals can be emitted
// as tagged tuples in the correct order.
type emitter struct {
	structs map[string][]string
	fset    *token.FileSet
}

// File parses Go source and emits an Erlang module string, along with the
// module name (the Go package name) so callers don't need to re-parse the
// emitted header to recover it.
func File(src string) (string, string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		return "", "", err
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
						return "", "", fmt.Errorf("struct %s field %s is lowercase-leading; Erlang variables must be uppercase",
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
		if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
			return "", "", fmt.Errorf("unsupported function %s: parameters are not yet supported (echo subset); see the 0.2.x roadmap", fn.Name.Name)
		}
		name := strings.ToLower(fn.Name.Name)
		if prev, ok := seen[name]; ok {
			return "", "", fmt.Errorf("functions %s and %s both map to Erlang atom %s (duplicate clause); rename one",
				prev, fn.Name.Name, name)
		}
		seen[name] = fn.Name.Name
		if fn.Name.IsExported() {
			exports = append(exports, name+"/0")
		}
		stmts, err := em.emitBody(fn.Body)
		if err != nil {
			return "", "", err
		}
		// A single-statement body that emits on one line gets a one-line
		// clause (e.g. `start() -> register(...).`). An empty body (which
		// emits the "ok" placeholder) and multi-statement bodies keep the
		// standard indented multi-line form.
		if fn.Body != nil && len(fn.Body.List) == 1 && !strings.Contains(stmts, "\n") {
			fmt.Fprintf(&bodies, "\n%s() -> %s.\n", name, stmts)
		} else {
			fmt.Fprintf(&bodies, "\n%s() ->\n%s.\n", name, indent(stmts))
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
	for typeName, ms := range methods {
		initFn := methodNamed(ms, "Init")
		if initFn == nil {
			return "", "", fmt.Errorf("type %s has methods but no Init; not a recognized gen_server", typeName)
		}
		behaviour = "-behaviour(gen_server).\n"
		exports = append(exports, "init/1")
		results, err := returnExprs(initFn.Body)
		if err != nil {
			return "", "", em.errorf(initFn, "Init: %s", err)
		}
		state, err := em.emitExpr(results[0])
		if err != nil {
			return "", "", err
		}
		fmt.Fprintf(&callbacks, "\ninit(_) -> {ok, %s}.\n", state)

		if hc := methodNamed(ms, "HandleCall"); hc != nil {
			exports = append(exports, "handle_call/3")
			// Param -> uppercase Erlang variable (guiding principle: reject lowercase).
			if hc.Type.Params == nil || len(hc.Type.Params.List) != 1 || len(hc.Type.Params.List[0].Names) != 1 {
				return "", "", em.errorf(hc, "HandleCall must take exactly one parameter")
			}
			param := hc.Type.Params.List[0].Names[0].Name
			if !token.IsExported(param) {
				return "", "", em.errorf(hc, "HandleCall parameter %s is lowercase-leading; Erlang variables must be uppercase", param)
			}
			// Receiver state head-pattern: {state, F1, F2, ...} binding all fields.
			statePat := []string{strings.ToLower(typeName)}
			statePat = append(statePat, em.structs[typeName]...)
			pattern := "{" + strings.Join(statePat, ", ") + "}"
			// Body: return Reply, NewState -> {reply, Reply, NewState}.
			hcResults, err := returnExprs(hc.Body)
			if err != nil {
				return "", "", em.errorf(hc, "HandleCall: %s", err)
			}
			if len(hcResults) != 2 {
				return "", "", em.errorf(hc, "HandleCall must return (reply, state)")
			}
			reply, err := em.emitExpr(hcResults[0])
			if err != nil {
				return "", "", err
			}
			next, err := em.emitExpr(hcResults[1])
			if err != nil {
				return "", "", err
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
	return b.String(), f.Name.Name, nil
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
		pre, err := em.emitStmts(body.List[:i])
		if err != nil {
			return "", err
		}
		pat, _, _ := em.receiveHead(body.List[i:])
		inner, err := em.emitStmts(body.List[i+1:])
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
	return em.emitStmts(body.List)
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

// emitStmts emits a list of statements as a comma-separated Erlang
// expression sequence.
func (em *emitter) emitStmts(list []ast.Stmt) (string, error) {
	var parts []string
	for _, s := range list {
		e, err := em.emitStmt(s)
		if err != nil {
			return "", err
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ",\n"), nil
}

// receiveHead recognizes a leading `x := otp.Receive().(T)` statement and
// returns the Erlang tuple pattern for T (atom = lowercased type name, each
// field bound to its capitalized field name) plus the remaining statements.
func (em *emitter) receiveHead(list []ast.Stmt) (pattern string, rest []ast.Stmt, ok bool) {
	as, ok := list[0].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 {
		return "", nil, false
	}
	ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return "", nil, false
	}
	call, ok := ta.X.(*ast.CallExpr)
	if !ok || !isOtpCall(call, "Receive") {
		return "", nil, false
	}
	typ, ok := ta.Type.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	parts := []string{strings.ToLower(typ.Name)}
	for _, fld := range em.structs[typ.Name] {
		parts = append(parts, fld) // field name is the bound Erlang variable
	}
	return "{" + strings.Join(parts, ", ") + "}", list[1:], true
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

func (em *emitter) emitCall(c *ast.CallExpr) (string, error) {
	// bare self-call: Serve()
	if id, ok := c.Fun.(*ast.Ident); ok {
		if len(c.Args) != 0 {
			return "", em.errorf(c, "unsupported call %s with arguments: only nullary self-calls are in the subset (see the 0.2.x roadmap)", id.Name)
		}
		return strings.ToLower(id.Name) + "()", nil
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
	if sel.Sel.Name == "StartServer" {
		name, err := em.emitExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("gen_server:start_link({local, %s}, ?MODULE, [], [])", unquoteAtom(name)), nil
	}
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		s, err := em.emitExpr(a)
		if err != nil {
			return "", err
		}
		args[i] = s
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
