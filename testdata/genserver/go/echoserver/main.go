package echoserver

import "go.muehmer.eu/wintermute/pkg/otp"

type State struct{ Count int }

func (State) Init() State { return State{Count: 0} }
func (s State) HandleCall(Req string) (string, State) {
	return Req, State{Count: s.Count + 1}
}

func Start() { otp.StartServer("echo", State{}) }
