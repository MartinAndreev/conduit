package memoryinfra

import (
	"fmt"
	"os"
	"testing"
)

// TestMain isolates the git the reader starts from the developer's global
// configuration: the reader keeps HOME and XDG_CONFIG_HOME, so they point at
// an empty directory here.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "memoryinfra-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
