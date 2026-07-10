package erlang

// MissingBuildTools returns the executables required to build OTP from source
// that lookPath cannot find. Pass exec.LookPath in production; inject a fake in
// tests. A C compiler is satisfied by either "cc" or "gcc".
//
// ponytail: check binaries via LookPath; the ncurses/openssl -dev headers are
// named in the install error rather than probed (not stdlib-trivial across
// distros). Add header probing if a missing lib bites despite the compiler
// being present.
func MissingBuildTools(lookPath func(string) (string, error)) []string {
	found := func(name string) bool { _, err := lookPath(name); return err == nil }

	var missing []string
	if !found("cc") && !found("gcc") {
		missing = append(missing, "cc/gcc")
	}
	// tar is used to extract the OTP source tarball; the rest build it.
	for _, tool := range []string{"make", "m4", "perl", "tar"} {
		if !found(tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}
