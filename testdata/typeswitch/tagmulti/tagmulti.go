// Package tagmulti is a 0.3.7 fixture exercising a multi-type case (case Ping,
// Pong:) and a primitive arm in one plain-value type switch. It transpiles,
// compiles with erlc, and runs to a checked result.
package tagmulti

type Ping struct{ Seq int }
type Pong struct{ Seq int }

// Classify returns 1 for a Ping or a Pong (multi-type case, whole value ignored),
// the int itself for an int, and 0 otherwise.
func Classify(M any) int {
	switch V := M.(type) {
	case Ping, Pong:
		return 1
	case int:
		return V
	default:
		return 0
	}
}
