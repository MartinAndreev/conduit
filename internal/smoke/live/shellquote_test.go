package live

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/Users/u/.conduit/a.txt": `'/Users/u/.conduit/a.txt'`,
		"/a b/c":                  `'/a b/c'`,
		"/it's/x":                 `'/it'\''s/x'`,
		"/$(touch x)/y":           `'/$(touch x)/y'`,
		"/`id`/y":                 "'/`id`/y'",
		"":                        `''`,
	}
	for value, want := range tests {
		if got := shellQuote(value); got != want {
			t.Fatalf("shellQuote(%q) = %s, want %s", value, got, want)
		}
	}
}

// The quoted word must reach a real POSIX shell as exactly one literal argument.
func TestShellQuoteRoundTripsThroughSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell")
	}
	for _, value := range []string{"/a b/c", "/it's/x", "/$(echo pwned)/y", "/`echo pwned`/y", "/*", "/a\nb", "/\"q\"\\"} {
		out, err := exec.Command("/bin/sh", "-c", "printf '%s' "+shellQuote(value)).Output()
		if err != nil || string(out) != value {
			t.Fatalf("round trip of %q = %q, %v", value, out, err)
		}
	}
}
