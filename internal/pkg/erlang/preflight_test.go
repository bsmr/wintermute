package erlang

import (
	"errors"
	"testing"
)

func TestMissingBuildTools(t *testing.T) {
	found := func(string) (string, error) { return "/usr/bin/x", nil }
	notFound := func(string) (string, error) { return "", errors.New("not found") }

	if m := MissingBuildTools(found); len(m) != 0 {
		t.Fatalf("all tools present: want none missing, got %v", m)
	}

	got := MissingBuildTools(notFound)
	if len(got) != 5 { // cc/gcc, make, m4, perl, tar
		t.Fatalf("none present: got %v, want 5 missing", got)
	}

	// gcc alone satisfies the compiler requirement.
	gccOnly := func(name string) (string, error) {
		if name == "gcc" {
			return "/usr/bin/gcc", nil
		}
		return "", errors.New("not found")
	}
	for _, m := range MissingBuildTools(gccOnly) {
		if m == "cc/gcc" {
			t.Fatalf("gcc should satisfy the compiler requirement; got %v", MissingBuildTools(gccOnly))
		}
	}
}
