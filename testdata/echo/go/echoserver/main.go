package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Serve() {
	req := otp.Receive().(Echo)
	otp.Send(req.From, Ok{Text: req.Text})
	Serve()
}

func Start() { otp.Register("echo", otp.Spawn(Serve)) }
