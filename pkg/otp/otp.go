// Package otp is the Erlang/OTP-in-Go marker API. Programs written with it are
// valid Go for tooling, but execute only after transpilation to Erlang; the
// bodies here panic if run natively.
package otp

const transpileOnly = "otp: transpile-only marker; run via the Wintermute transpiler"

// Pid is an opaque Erlang process identifier.
type Pid struct{ _ struct{} }

func Self() Pid                   { panic(transpileOnly) } // -> self()
func Spawn(fn func()) Pid         { panic(transpileOnly) } // -> spawn(fun ... end)
func Register(name string, p Pid) { panic(transpileOnly) } // -> register(name, Pid)
func Whereis(name string) Pid     { panic(transpileOnly) } // -> whereis(name)
func Send(to Pid, msg any)        { panic(transpileOnly) } // -> To ! Msg
func Receive() any                { panic(transpileOnly) } // -> receive <clause> end
func Print(s string)              { panic(transpileOnly) } // -> io:format("~s~n", [S])
