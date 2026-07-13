// Package dispatch is a 0.3.4 type-switch-receive fixture: a function receives a
// tagged message and dispatches on its type, returning a word per type. It
// transpiles, compiles with erlc, and runs to a checked result.
package dispatch

import "go.muehmer.eu/wintermute/pkg/otp"

type Ping struct{ Data string }
type Pong struct{ Data string }

// Handle receives one message and returns a word identifying its type. With the
// message already in the mailbox, the selective receive matches immediately.
func Handle() string {
	switch v := otp.Receive().(type) {
	case Ping:
		return v.Data
	case Pong:
		return v.Data
	default:
		return "other"
	}
}
