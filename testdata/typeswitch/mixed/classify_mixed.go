// Package mixed is a 0.3.6 fixture mixing a primitive-guard case, a struct
// case, and a default in one plain-value type switch. It transpiles, compiles
// with erlc, and runs: an int hits the is_integer guard (returning the whole
// aliased value V), a Ping tuple hits the struct clause (returning its field),
// anything else hits the default.
package mixed

type Ping struct{ Seq int }

// Classify branches on the dynamic type of M: an int returns itself (the
// whole-alias V under an is_integer guard), a Ping returns its Seq field, and
// any other value returns 0 (the default catch-all).
func Classify(M any) int {
	switch V := M.(type) {
	case int:
		return V
	case Ping:
		return V.Seq
	default:
		return 0
	}
}
