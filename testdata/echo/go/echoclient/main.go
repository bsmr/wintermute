package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

type Echo struct {
	From otp.Pid
	Text string
}
type Ok struct {
	Text string
}

func Main() {
	otp.Send(otp.Whereis("echo"), Echo{From: otp.Self(), Text: "hello"})
	reply := otp.Receive().(Ok)
	otp.Print(reply.Text)
}
