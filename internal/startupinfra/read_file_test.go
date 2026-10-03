package startupinfra

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
)

type readTestFile struct {
	io.Reader
	info     os.FileInfo
	statErr  error
	closeErr error
	closed   bool
}

func (f *readTestFile) Stat() (os.FileInfo, error) { return f.info, f.statErr }
func (f *readTestFile) Close() error               { f.closed = true; return f.closeErr }

func TestReadFileQueryPreservesCausesAndIdentityDistinction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	other := filepath.Join(t.TempDir(), "other.yaml")
	for _, p := range []string{path, other} {
		if err := os.WriteFile(p, []byte("contents"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	otherInfo, err := os.Lstat(other)
	if err != nil {
		t.Fatal(err)
	}
	statCause := &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	readCause := errors.New("read failure")
	closeCause := errors.New("close failure")
	for _, tc := range []struct {
		name                               string
		actual                             os.FileInfo
		statErr, errorRead, closeErr, want error
	}{
		{name: "stat preserves native cause", statErr: statCause, want: statCause},
		{name: "identity changed", actual: otherInfo, want: errConfigurationChanged},
		{name: "read preserves cause", actual: info, errorRead: readCause, want: readCause},
		{name: "close preserves cause", actual: info, closeErr: closeCause, want: closeCause},
		{name: "read failure precedes close failure", actual: info, errorRead: readCause, closeErr: closeCause, want: readCause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reader io.Reader = bytes.NewReader([]byte("contents"))
			if tc.errorRead != nil {
				reader = failingConfigReader{tc.errorRead}
			}
			file := &readTestFile{Reader: reader, info: tc.actual, statErr: tc.statErr, closeErr: tc.closeErr}
			source := configurationFileSource{lstat: func(string) (os.FileInfo, error) { return info, nil }, open: func(string) (configurationFile, error) { return file, nil }}
			facts := readConfigurationFile(context.Background(), path, source)
			if facts.kind != fileUnreadable || !errors.Is(facts.err, tc.want) {
				t.Fatalf("facts=%+v want=%v", facts, tc.want)
			}
			if !file.closed {
				t.Fatal("query leaked open file")
			}
		})
	}
}

type failingConfigReader struct{ err error }

func (r failingConfigReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadFileQueryMissingOnOpenIsUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	source := configurationFileSource{lstat: func(string) (os.FileInfo, error) { return info, nil }, open: func(string) (configurationFile, error) { return nil, os.ErrNotExist }}
	facts := readConfigurationFile(context.Background(), path, source)
	if facts.kind != fileUnreadable || !errors.Is(facts.err, os.ErrNotExist) {
		t.Fatal(facts)
	}
}

func TestReadConfigurationSizeBoundaryAndCancellation(t *testing.T) {
	data, err := configuration.Encode(initialConfig())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	// A comment fills the file without changing its valid YAML meaning.
	data = append(data, '#')
	data = append(data, bytes.Repeat([]byte("x"), maxConfigBytes-len(data))...)
	for _, tc := range []struct {
		name string
		data []byte
		want startup.ReadKind
	}{
		{"at limit", data, startup.Valid},
		{"over limit", append(append([]byte(nil), data...), 'x'), startup.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			result := (Files{}).Read(context.Background(), path)
			if result.Kind != tc.want {
				t.Fatal(result)
			}
			if tc.want == startup.Invalid && !errors.Is(result.Err, configuration.ErrInvalid) {
				t.Fatal(result.Err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := (Files{}).Read(ctx, filepath.Join(t.TempDir(), "missing"))
	if result.Kind != startup.Unreadable || !errors.Is(result.Err, context.Canceled) {
		t.Fatal(result)
	}
}
