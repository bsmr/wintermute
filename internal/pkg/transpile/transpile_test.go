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

func TestFile_NullaryCallWithArgsErrors(t *testing.T) {
	src := `package m
func Boot() { Helper("x") }
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for bare-identifier call with arguments, got nil")
	}
}

func TestFile_FunctionWithParamsErrors(t *testing.T) {
	src := `package m
func Boot(x string) {}
`
	_, _, err := File(src)
	if err == nil {
		t.Fatal("want error for function with parameters, got nil")
	}
	if !strings.Contains(err.Error(), "0.2.x roadmap") {
		t.Fatalf("error should point at the roadmap, got: %v", err)
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
func Boot() { 1 - 2 }
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

func TestEmitExpr_NonAddBinaryErrors(t *testing.T) {
	em := &emitter{structs: map[string][]string{}}
	expr := &ast.BinaryExpr{X: &ast.Ident{Name: "A"}, Op: token.SUB, Y: &ast.Ident{Name: "B"}}
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
