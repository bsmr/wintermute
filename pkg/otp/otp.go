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
