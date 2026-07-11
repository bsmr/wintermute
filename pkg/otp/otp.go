// Package otp is the Erlang/OTP-in-Go marker API. Programs written with it are
// valid Go for tooling, but execute only after transpilation to Erlang; the
// bodies here panic if run natively.
package otp

// transpileOnly panics with an actionable message naming the marker and the
// fix. These markers exist so the source is valid Go (gopls/go vet work); they
// are meant to be transpiled, not run.
// ponytail: a //go:build guard would fail earlier but hides the symbols and
// breaks the valid-Go tooling thesis — rejected deliberately.
func transpileOnly(sym string) {
	panic("otp." + sym + ": transpile-only marker — compile with `wm build`, do not run natively")
}

// Pid is an opaque Erlang process identifier.
type Pid struct{ _ struct{} }

func Self() Pid                   { transpileOnly("Self"); return Pid{} }    // -> self()
func Spawn(fn func()) Pid         { transpileOnly("Spawn"); return Pid{} }   // -> spawn(fun ... end)
func Register(name string, p Pid) { transpileOnly("Register") }              // -> register(name, Pid)
func Whereis(name string) Pid     { transpileOnly("Whereis"); return Pid{} } // -> whereis(name)

func RegisterGlobal(name string, p Pid) { transpileOnly("RegisterGlobal") }              // -> global:register_name(name, Pid)
func WhereisGlobal(name string) Pid     { transpileOnly("WhereisGlobal"); return Pid{} } // -> global:whereis_name(name)
func Send(to Pid, msg any)              { transpileOnly("Send") }                        // -> To ! Msg
func Receive() any                      { transpileOnly("Receive"); return nil }         // -> receive <clause> end
func Print(s string)                    { transpileOnly("Print") }                       // -> io:format("~s~n", [S])

func StartServer(name string, init any) { transpileOnly("StartServer") }      // -> gen_server:start_link({local,name}, ?MODULE, [], [])
func Call(name string, req any) any     { transpileOnly("Call"); return nil } // -> gen_server:call(name, Req)

func StartServerGlobal(name string, init any) { transpileOnly("StartServerGlobal") } // -> gen_server:start_link({global,name}, ?MODULE, [], [])
func CallGlobal(name string, req any) any      { transpileOnly("CallGlobal"); return nil } // -> gen_server:call({global,name}, Req)

// Child describes one supervised process for a supervisor's Init. Start is the
// child's start function (e.g. echoserver.Start); it maps to the child spec MFA.
type Child struct {
	ID    string
	Start func()
}

func StartSupervisor(sup any) Pid { transpileOnly("StartSupervisor"); return Pid{} } // -> Sup:start_link()
