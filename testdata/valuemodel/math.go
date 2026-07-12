// Package math is a 0.3.1 value-model fixture: parameters, a local binding,
// and a call with arguments. It transpiles and must compile with erlc.
package math

// Add returns the sum of X and Y.
func Add(X, Y int) int { return X + Y }

// Double returns X + X via a local binding and a call with arguments.
func Double(X int) int {
	Z := Add(X, X)
	return Z
}
