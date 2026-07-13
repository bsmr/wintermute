package transpile

import (
	"go/ast"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestFile_ModuleAndExport(t *testing.T) {
	src := `package echoserver
func Serve() {}
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoserver).\n-export([serve/0]).\n\nserve() ->\n    ok.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFile_ClientBody(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() {
	otp.Send(otp.Whereis("echo"), "hello")
	otp.Print("done")
}
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "-module(echoclient).\n-export([main/0]).\n\n" +
		"main() ->\n" +
		"    whereis(echo) ! <<\"hello\">>,\n" +
		"    io:format(\"~s~n\", [<<\"done\">>]).\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFile_StructLiteralToTuple(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
}
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "whereis(echo) ! {echo, self(), <<\"hello\">>}") {
		t.Fatalf("missing tuple emission:\n%s", got)
	}
}

func TestFile_SpawnNonIdentErrors(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Boot() { otp.Spawn(makeFn()) }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for non-identifier otp.Spawn argument, got nil")
	}
}

func TestFile_ServerReceiveLoop(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
type Ok struct { Text string }
func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "serve() ->\n" +
		"    receive\n" +
		"        {echo, From, Text} ->\n" +
		"            From ! {ok, Text},\n" +
		"            serve()\n" +
		"    end.\n"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant contains:\n%s", got, want)
	}
}

func TestFile_GoldenServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/echo/go/echoserver/main.go")
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
		"start() -> register(echo, spawn(fun ?MODULE:serve/0)).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_StructLiteralMissingFieldErrors(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
type Echo struct { From otp.Pid; Text string }
func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self()})
}
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for struct literal omitting a declared field, got nil")
	}
}

// TestFile_CallWithArgsEmits: bare-identifier calls with arguments are now
// supported (0.3.1) — see TestModule_CallWithArgs and
// TestModule_SelfRecursionEmits — so this no longer errors.
func TestFile_CallWithArgsEmits(t *testing.T) {
	src := `package m
func Boot() { Helper("x") }
func Helper(S string) {}
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `helper(<<"x">>)`) {
		t.Fatalf("want helper call with argument, got:\n%s", got)
	}
}

// TestFile_FunctionWithParamsErrors: parameters are now supported (0.3.1), but
// a lowercase-leading name is still rejected since it would become an
// invalid (lowercase) Erlang variable — see TestModule_LowercaseParamRejected.
func TestFile_FunctionWithParamsErrors(t *testing.T) {
	src := `package m
func Boot(x string) {}
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for lowercase-leading parameter, got nil")
	}
	if !strings.Contains(err.Error(), "uppercase") {
		t.Fatalf("error should point at the uppercase requirement, got: %v", err)
	}
}

func TestFile_AtomCollisionErrors(t *testing.T) {
	src := `package m
func Foo() {}
func foo() {}
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for Foo/foo collapsing to the same Erlang atom, got nil")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Fatalf("error should name the colliding atom, got: %v", err)
	}
}

func TestFile_LowercaseFieldErrors(t *testing.T) {
	src := `package m
type Msg struct { text string }
func Serve() { m := otp.Receive().(Msg); otp.Print(m.text) }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for lowercase-leading struct field (invalid Erlang variable), got nil")
	}
	if !strings.Contains(err.Error(), "text") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}

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

func TestFile_LowercaseBareIdentErrors(t *testing.T) {
	src := `package m
func Boot() { otp.Print(x) }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for lowercase-leading bare identifier, got nil")
	}
	if !strings.Contains(err.Error(), "x") {
		t.Fatalf("error should name the identifier, got: %v", err)
	}
}

func TestFile_ErrorsCarryPosition(t *testing.T) {
	src := `package m
func Boot() { 1 << 2 }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for unsupported binary operator, got nil")
	}
	if !strings.Contains(err.Error(), "src.go:2") {
		t.Fatalf("error should carry a src.go:line position, got: %v", err)
	}
}

func TestFile_ReturnsModuleName(t *testing.T) {
	_, mod, err := File("package echoserver\nfunc Serve() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echoserver" {
		t.Fatalf("module = %q, want echoserver", mod)
	}
}

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

func TestEmitExpr_IntAndAdd(t *testing.T) {
	em := &emitter{structs: map[string][]string{}}
	// Count + 1
	expr := &ast.BinaryExpr{
		X:  &ast.Ident{Name: "Count"},
		Op: token.ADD,
		Y:  &ast.BasicLit{Kind: token.INT, Value: "1"},
	}
	got, err := em.emitExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Count + 1" {
		t.Fatalf("got %q, want %q", got, "Count + 1")
	}
}

// TestEmitExpr_UnsupportedBinaryErrors covers an operator still outside the
// binOp table (e.g. bit-shift), not merely a non-"+" one: 0.3.2 added the full
// arithmetic/comparison/boolean set (see TestModule_Operators).
func TestEmitExpr_UnsupportedBinaryErrors(t *testing.T) {
	em := &emitter{structs: map[string][]string{}}
	expr := &ast.BinaryExpr{X: &ast.Ident{Name: "A"}, Op: token.SHL, Y: &ast.Ident{Name: "B"}}
	if _, err := em.emitExpr(expr); err == nil {
		t.Fatal("want error for unsupported binary operator, got nil")
	}
}

func TestFile_GenServerCall(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `io:format("~s~n", [gen_server:call(echo, <<"hello">>)])`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFile_StartServer(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func Start() { otp.StartServer("echo", State{}) }
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFile_GenServerInit(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
var _ = otp.Self
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-behaviour(gen_server).",
		"init(_) -> {ok, {state, 0}}.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GenServerHandleCall(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) {
	return Req, State{Count: s.Count + 1}
}
var _ = otp.Self
`
	got, _, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"handle_call/3",
		"handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GenServerLowercaseParamErrors(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(req string) (string, State) { return req, s }
var _ = otp.Self
`
	if _, _, err := File(src); err == nil {
		t.Fatal("want error for lowercase-leading callback param, got nil")
	}
}

func TestFile_GoldenGenServer(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/genserver/go/echoserver/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-module(echoserver).",
		"-behaviour(gen_server).",
		"init(_) -> {ok, {state, 0}}.",
		"handle_call(Req, _From, {state, Count}) -> {reply, Req, {state, Count + 1}}.",
		"start() -> gen_server:start_link({local, echo}, ?MODULE, [], []).",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFile_GoldenGenServerClient(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/genserver/go/echoclient/main.go")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := File(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `main() -> io:format("~s~n", [gen_server:call(echo, <<"hello">>)]).`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSupervisorBehaviour(t *testing.T) {
	src := `package echosup
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echoserver"
type Sup struct{}
func (Sup) Init() []otp.Child {
	return []otp.Child{{ID: "echo", Start: echoserver.Start}}
}
`
	erl, mod, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echosup" {
		t.Fatalf("mod = %q, want echosup", mod)
	}
	for _, want := range []string{
		"-behaviour(supervisor).",
		"start_link() -> supervisor:start_link({local, echosup}, ?MODULE, []).",
		"init(_) -> {ok, {{one_for_one, 1, 5}, [{echo, {echoserver, start, []}, permanent, 5000, worker, [echoserver]}]}}.",
	} {
		if !strings.Contains(erl, want) {
			t.Fatalf("missing %q in:\n%s", want, erl)
		}
	}
}

func TestApplicationBehaviour(t *testing.T) {
	src := `package echoapp
import "go.muehmer.eu/wintermute/pkg/otp"
import "example/echosup"
type App struct{}
func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
`
	erl, mod, err := File(src)
	if err != nil {
		t.Fatal(err)
	}
	if mod != "echoapp" {
		t.Fatalf("mod = %q, want echoapp", mod)
	}
	for _, want := range []string{
		"-behaviour(application).",
		"start(_Type, _Args) -> echosup:start_link().",
		"stop(_State) -> ok.",
	} {
		if !strings.Contains(erl, want) {
			t.Fatalf("missing %q in:\n%s", want, erl)
		}
	}
}

func TestModuleReportsBehaviourAndRegistered(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServer("echo", State{}) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Module != "echoserver" || r.Behaviour != "gen_server" {
		t.Fatalf("got module=%q behaviour=%q", r.Module, r.Behaviour)
	}
	if len(r.Registered) != 1 || r.Registered[0] != "echo" {
		t.Fatalf("registered = %v, want [echo]", r.Registered)
	}
}

func TestAppResource(t *testing.T) {
	got := AppResource("echoapp", "0.2.3",
		[]string{"echoapp", "echosup", "echoserver"}, []string{"echo"})
	want := `{application, echoapp,
 [{description, "echoapp"},
  {vsn, "0.2.3"},
  {modules, [echoapp, echosup, echoserver]},
  {registered, [echo]},
  {applications, [kernel, stdlib]},
  {mod, {echoapp, []}}]}.
`
	if got != want {
		t.Fatalf("AppResource mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestModuleOutputIsDeterministic(t *testing.T) {
	// Two method-carrying types force the methods-map iteration to matter;
	// map order is randomized per range, so without sorting the emitted
	// callback order varies across runs. Assert a single distinct output.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Aaa struct{ X int }
func (Aaa) Init() Aaa { return Aaa{X: 0} }
type Bbb struct{ Y int }
func (Bbb) Init() Bbb { return Bbb{Y: 0} }
`
	seen := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		erl, _, err := File(src)
		if err != nil {
			t.Fatal(err)
		}
		seen[erl] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("non-deterministic output: %d distinct results across 50 runs", len(seen))
	}
}

func TestOtpCallWrongArityErrors(t *testing.T) {
	// otp.Call takes two args; one arg parses fine (no type-checking) and must
	// yield a clean positioned error, not an index-out-of-range panic.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func Bad() { otp.Call("x") }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for otp.Call with wrong arity, got nil")
	}
	if !strings.Contains(err.Error(), "Call") {
		t.Fatalf("error should name the call, got: %v", err)
	}
}

func TestTranspileStartServerGlobal(t *testing.T) {
	src := `package echoserver
import "go.muehmer.eu/wintermute/pkg/otp"
type State struct{ Count int }
func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) { return Req, State{Count: s.Count + 1} }
func Start() { otp.StartServerGlobal("echo", State{Count: 0}) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Erl, "gen_server:start_link({global, echo}, ?MODULE, [], [])") {
		t.Fatalf("missing global start_link:\n%s", r.Erl)
	}
	if len(r.Registered) != 0 {
		t.Fatalf("global server must not populate Registered, got %v", r.Registered)
	}
}

func TestTranspileCallGlobal(t *testing.T) {
	src := `package echoclient
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.Print(otp.CallGlobal("echo", "hello").(string)) }
`
	r, err := Module(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Erl, `gen_server:call({global, echo}, <<"hello">>)`) {
		t.Fatalf("missing global call:\n%s", r.Erl)
	}
}

func TestTranspileCallGlobalWrongArity(t *testing.T) {
	src := `package c
import "go.muehmer.eu/wintermute/pkg/otp"
func Main() { otp.CallGlobal("echo") }
`
	if _, err := Module(src); err == nil {
		t.Fatal("expected positioned arity error, got nil")
	}
}

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
	if err == nil || !strings.Contains(err.Error(), "if branch") {
		t.Fatalf("want early-return error pointing at if branch, got %v", err)
	}
}

func TestModule_ReturnBeforeReceiveRejected(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func F(Y int) int {
	return Y
	M := otp.Receive().(Msg)
	otp.Print("after")
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "early return") {
		t.Fatalf("want early-return rejection, got %v", err)
	}
}

// A return as the last statement of a receive clause body is a legitimate
// trailing return (the clause body is the function's tail): it must be accepted
// and emit the returned expression as the clause value. This is the positive
// counterpart to TestModule_ReturnBeforeReceiveRejected — the exact boundary the
// isTail fix guards.
func TestModule_ReturnInReceiveClauseBody(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle() int {
	M := otp.Receive().(Msg)
	return M.X
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "{msg, X} ->") || !strings.Contains(r.Erl, "\n            X\n") {
		t.Errorf("want receive clause {msg, X} -> X, got:\n%s", r.Erl)
	}
}

// A function parameter sharing a name with a receive-pattern field must be
// rejected: in Erlang a pattern variable that is already bound is an equality
// match, not a fresh binding, so silently reusing the param would change the
// receive's semantics instead of erroring.
func TestModule_ParamCollidesWithReceiveField(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle(X int) int {
	M := otp.Receive().(Msg)
	return X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("want collision rejection, got %v", err)
	}
}

// A later `:=` binding that reuses a receive-pattern field name must be
// rejected by the same rebinding guard that already covers param/param and
// binding/binding collisions.
func TestModule_BindingCollidesWithReceiveField(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle(Y int) int {
	M := otp.Receive().(Msg)
	X := Y
	return X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want rebinding rejection, got %v", err)
	}
}

// The 0.3.1-analog seam: a `:=` inside an if branch, within a receive handler,
// colliding with a receive-pattern field. The branch's bound snapshot must
// include the receive fields, so this is rejected — not silently emitted as an
// Erlang equality match. (Guards against the exact class the Copilot gate caught.)
func TestModule_BindingInBranchCollidesWithReceiveField(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle() int {
	M := otp.Receive().(Msg)
	if M.X == 0 {
		X := 5
		return X
	}
	return M.X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want already-bound rejection, got %v", err)
	}
}

// Positive counterpart: an if in tail position inside a receive clause body is
// accepted and emits a case over the received field.
func TestModule_IfInReceiveClauseBody(t *testing.T) {
	src := `package demo
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle() int {
	M := otp.Receive().(Msg)
	if M.X == 0 {
		return 1
	}
	return M.X
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "case X =:= 0 of") {
		t.Errorf("want case over the received field, got:\n%s", r.Erl)
	}
}

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

// An unnamed parameter (valid Go: `func F(int, string) int`) has no name to
// become an Erlang variable, and silently dropping it from the parameter list
// would emit the wrong arity (f/0 instead of f/2) — reject it instead.
func TestModule_UnnamedParamRejected(t *testing.T) {
	src := `package demo
func F(int, string) int { return 1 }`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "unnamed") {
		t.Fatalf("want unnamed-parameter rejection, got %v", err)
	}
}

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

func TestModule_Operators(t *testing.T) {
	cases := []struct{ goExpr, erl string }{
		{"A - B", "a(A, B) -> A - B."},
		{"A * B", "a(A, B) -> A * B."},
		{"A / B", "a(A, B) -> A div B."},
		{"A % B", "a(A, B) -> A rem B."},
		{"A == B", "a(A, B) -> A =:= B."},
		{"A != B", "a(A, B) -> A =/= B."},
		{"A < B", "a(A, B) -> A < B."},
		{"A <= B", "a(A, B) -> A =< B."},
		{"A > B", "a(A, B) -> A > B."},
		{"A >= B", "a(A, B) -> A >= B."},
		{"A && B", "a(A, B) -> A andalso B."},
		{"A || B", "a(A, B) -> A orelse B."},
	}
	for _, c := range cases {
		src := "package m\nfunc A(A, B int) int { return " + c.goExpr + " }"
		r, err := Module(src)
		if err != nil {
			t.Fatalf("%s: Module: %v", c.goExpr, err)
		}
		if !strings.Contains(r.Erl, c.erl) {
			t.Errorf("%s: want %q, got:\n%s", c.goExpr, c.erl, r.Erl)
		}
	}
}

func TestModule_UnaryNot(t *testing.T) {
	src := `package m
func F(A bool) bool { return !A }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "f(A) -> not A.") {
		t.Errorf("want not A, got:\n%s", r.Erl)
	}
}

func TestModule_PrecedenceParens(t *testing.T) {
	// A binary operand keeps its grouping; a single operator stays bare.
	src := `package m
func F(A, B, C int) int { return (A + B) * C }`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	if !strings.Contains(r.Erl, "f(A, B, C) -> (A + B) * C.") {
		t.Errorf("want (A + B) * C, got:\n%s", r.Erl)
	}
}

func TestModule_UnsupportedOperatorRejected(t *testing.T) {
	src := `package m
func F(A, B int) int { return A << B }`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("want unsupported-operator error, got %v", err)
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

func TestModule_IfElse(t *testing.T) {
	src := `package m
func Sign(N int) int {
	if N == 0 {
		return 0
	} else {
		return 1
	}
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	for _, want := range []string{"case N =:= 0 of", "true -> 0", "false -> 1", "end"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q, got:\n%s", want, r.Erl)
		}
	}
}

func TestModule_BareIfBaseCase(t *testing.T) {
	src := `package m
func Fact(N int) int {
	if N == 0 {
		return 1
	}
	return N * Fact(N-1)
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	for _, want := range []string{"case N =:= 0 of", "true -> 1", "false -> N * fact(N - 1)"} {
		if !strings.Contains(r.Erl, want) {
			t.Errorf("want %q, got:\n%s", want, r.Erl)
		}
	}
}

func TestModule_BranchSiblingReuse(t *testing.T) {
	// Z bound fresh in both branches — legal (independent Erlang case-clause scopes).
	src := `package m
func F(N int) int {
	if N == 0 {
		Z := 1
		return Z
	}
	Z := 2
	return Z
}`
	r, err := Module(src)
	if err != nil {
		t.Fatalf("want sibling reuse accepted, got %v", err)
	}
	if !strings.Contains(r.Erl, "true -> Z = 1") || !strings.Contains(r.Erl, "false -> Z = 2") {
		t.Errorf("got:\n%s", r.Erl)
	}
}

func TestModule_BranchOuterCollisionRejected(t *testing.T) {
	// Z collides with the parameter Z — rejected (outer binding visible in the branch).
	src := `package m
func F(Z int) int {
	if Z == 0 {
		Z := 1
		return Z
	}
	return Z
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("want outer-collision rejection, got %v", err)
	}
}

func TestModule_ElseIfRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	} else if N == 1 {
		return 1
	}
	return 2
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "else-if") {
		t.Fatalf("want else-if rejection, got %v", err)
	}
}

func TestModule_BareIfNoContinuationRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "false branch") {
		t.Fatalf("want bare-if-no-continuation rejection, got %v", err)
	}
}

func TestModule_BareIfNonTerminatingThenRejected(t *testing.T) {
	// A bare-if then-branch that does not return would, in Go, fall through to
	// the continuation; an Erlang terminal case clause cannot express that, so
	// it must be rejected rather than silently returning the branch's value.
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F(N int) int {
	if N == 0 {
		otp.Print("zero")
	}
	return N
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "fall through") {
		t.Fatalf("want non-terminating-then rejection, got %v", err)
	}
}

func TestModule_UnreachableAfterIfElseRejected(t *testing.T) {
	src := `package m
func F(N int) int {
	if N == 0 {
		return 0
	} else {
		return 1
	}
	return 2
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("want unreachable rejection, got %v", err)
	}
}

func TestModule_EmptyThenBranchRejected(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F(N int) {
	if N == 0 {
	} else {
		otp.Print("x")
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "empty block") {
		t.Fatalf("want empty-then rejection, got %v", err)
	}
}

func TestModule_EmptyElseBranchRejected(t *testing.T) {
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
func F(N int) {
	if N == 0 {
		otp.Print("x")
	} else {
	}
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "empty block") {
		t.Fatalf("want empty-else rejection, got %v", err)
	}
}

func TestModule_IfNonTailRejected(t *testing.T) {
	// An if in the pre-receive slice is not in tail position (isTail=false).
	src := `package m
import "go.muehmer.eu/wintermute/pkg/otp"
type Msg struct { X int }
func Handle(N int) int {
	if N == 0 {
		return 0
	}
	M := otp.Receive().(Msg)
	return M.X
}`
	_, err := Module(src)
	if err == nil || !strings.Contains(err.Error(), "tail position") {
		t.Fatalf("want non-tail-if rejection, got %v", err)
	}
}
