//go:build windows

package projectinfra

import (
	"context"
	"testing"
)

func TestWindowsVolumeRoots(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`c:\`, `C:\`},
		{`\\server\share\`, `\\SERVER\SHARE\`},
	} {
		got, err := realCasePath(context.Background(), tc.input, directorySource{})
		if err != nil || got != tc.want {
			t.Fatal(got, err, tc.want)
		}
	}
}
