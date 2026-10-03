package startupinfra

import (
	"errors"
	"io"
	"os"
	"runtime"
	"testing"
)

type failedTempFile struct {
	written                     int
	writeErr, syncErr, closeErr error
	calls                       []string
}

func (f *failedTempFile) Write([]byte) (int, error) {
	f.calls = append(f.calls, "write")
	return f.written, f.writeErr
}
func (f *failedTempFile) Sync() error  { f.calls = append(f.calls, "sync"); return f.syncErr }
func (f *failedTempFile) Close() error { f.calls = append(f.calls, "close"); return f.closeErr }

func TestWriteAndCloseConfigurationTempFailureBoundaries(t *testing.T) {
	writeCause := errors.New("write failed")
	syncCause := errors.New("sync failed")
	closeCause := errors.New("close failed")
	for _, tc := range []struct {
		name                              string
		written                           int
		writeErr, syncErr, closeErr, want error
		calls                             string
	}{
		{"success", 3, nil, nil, nil, nil, "write sync close"},
		{"partial write", 1, writeCause, nil, nil, writeCause, "write close"},
		{"short write", 1, nil, nil, nil, io.ErrShortWrite, "write close"},
		{"sync failure", 3, nil, syncCause, nil, syncCause, "write sync close"},
		{"close failure", 3, nil, nil, closeCause, closeCause, "write sync close"},
		{"write and close failure", 1, writeCause, nil, closeCause, writeCause, "write close"},
		{"sync and close failure", 3, nil, syncCause, closeCause, syncCause, "write sync close"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := &failedTempFile{written: tc.written, writeErr: tc.writeErr, syncErr: tc.syncErr, closeErr: tc.closeErr}
			err := writeAndCloseConfigurationTemp(file, []byte("abc"))
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.closeErr != nil && !errors.Is(err, tc.closeErr) {
				t.Fatal("lost close failure", err)
			}
			actual := ""
			for i, call := range file.calls {
				if i > 0 {
					actual += " "
				}
				actual += call
			}
			if actual != tc.calls {
				t.Fatalf("operations=%s want=%s", actual, tc.calls)
			}
		})
	}
}

func TestPrepareConfigurationTempReturnsClosedCompleteFile(t *testing.T) {
	directory := t.TempDir()
	name, err := prepareConfigurationTemp(directory, []byte("complete content"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(name)
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "complete content" {
		t.Fatal(string(data), err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("temp permissions=%v", info.Mode())
	}
}
