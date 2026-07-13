// Package classify is a 0.3.3 switch fixture: a tagged expression switch that
// transpiles, compiles with erlc, and runs to a checked result.
package classify

// Name maps a small int to a word via a switch with a default.
func Name(N int) string {
	switch N {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "many"
	}
}
