// Package classify is a 0.3.5 plain-value type-switch fixture: a function
// classifies a value's dynamic type and returns its Data field, so each
// clause's field binding is observable in the result. It transpiles, compiles
// with erlc, and runs to a checked result.
package classify

type Ping struct{ Data int }
type Pong struct{ Data int }

// Classify branches on the dynamic type of M and returns its Data field,
// proving each clause fires with its own field binding.
func Classify(M any) int {
	switch V := M.(type) {
	case Ping:
		return V.Data
	case Pong:
		return V.Data
	default:
		return 0
	}
}
