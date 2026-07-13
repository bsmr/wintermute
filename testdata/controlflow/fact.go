// Package fact is a 0.3.2 control-flow fixture: a runnable recursive factorial
// exercising operators (=:=, *, -) and a bare-if base case. It transpiles,
// compiles with erlc, and runs to a checked result.
package fact

// Fact returns N! via a base-case recursion.
func Fact(N int) int {
	if N == 0 {
		return 1
	}
	return N * Fact(N-1)
}
