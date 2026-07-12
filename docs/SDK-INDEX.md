# SDK Index

> Check this BEFORE implementing. If a capability exists here, use it.
> Regenerate: `/sdk-index`

---

## Go — `pkg/otp/`

### `go.muehmer.eu/wintermute/pkg/otp` — Package otp is the Erlang/OTP-in-Go marker API.

Package otp is the Erlang/OTP-in-Go marker API. Programs written with it are
valid Go for tooling, but execute only after transpilation to Erlang; the bodies
here panic if run natively.

**Exports:**
- `func Call(name string, req any) any`
- `func CallGlobal(name string, req any) any`
- `func Print(s string)`
- `func Receive() any`
- `func Register(name string, p Pid)`
- `func RegisterGlobal(name string, p Pid)`
- `func Send(to Pid, msg any)`
- `func StartServer(name string, init any)`
- `func StartServerGlobal(name string, init any)`
- `type Child struct{ ... }`
- `type Pid struct{ ... }`
- `func Self() Pid`
- `func Spawn(fn func()) Pid`
- `func StartSupervisor(sup any) Pid`
- `func Whereis(name string) Pid`
- `func WhereisGlobal(name string) Pid`
