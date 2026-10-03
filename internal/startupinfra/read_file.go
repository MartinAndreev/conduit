package startupinfra

import (
	"context"
	"errors"
	"io"
	"os"
)

type fileReadKind uint8

const (
	fileReadable fileReadKind = iota
	fileAbsent
	fileUnreadable
	fileTooLarge
)

// fileReadFacts owns its bytes; no open handle or mutable filesystem metadata
// escapes the query. Decoding and public startup classification happen in Read.
type fileReadFacts struct {
	kind fileReadKind
	data []byte
	err  error
}

type configurationFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

// The source is local to this query and permits deterministic filesystem races
// and failures in tests without changing the public Files adapter.
type configurationFileSource struct {
	lstat func(string) (os.FileInfo, error)
	open  func(string) (configurationFile, error)
}

func openConfigurationFile(path string) (configurationFile, error) { return os.Open(path) }

var errConfigurationNotRegular = errors.New("configuration is not a regular file")
var errConfigurationChanged = errors.New("configuration changed while opening")

func readConfigurationFile(ctx context.Context, path string, source configurationFileSource) (facts fileReadFacts) {
	if err := ctx.Err(); err != nil {
		return fileReadFacts{kind: fileUnreadable, err: err}
	}
	info, err := source.lstat(path)
	if cancellation := ctx.Err(); cancellation != nil {
		return fileReadFacts{kind: fileUnreadable, err: errors.Join(cancellation, err)}
	}
	if errors.Is(err, os.ErrNotExist) {
		return fileReadFacts{kind: fileAbsent}
	}
	if err != nil {
		return fileReadFacts{kind: fileUnreadable, err: err}
	}
	if !info.Mode().IsRegular() {
		return fileReadFacts{kind: fileUnreadable, err: errConfigurationNotRegular}
	}
	file, err := source.open(path)
	if err != nil {
		return fileReadFacts{kind: fileUnreadable, err: errors.Join(err, ctx.Err())}
	}
	defer func() {
		if err := file.Close(); err != nil {
			facts = fileReadFacts{kind: fileUnreadable, err: errors.Join(facts.err, err, ctx.Err())}
		}
	}()
	if err := ctx.Err(); err != nil {
		return fileReadFacts{kind: fileUnreadable, err: err}
	}
	actual, err := file.Stat()
	if cancellation := ctx.Err(); cancellation != nil {
		return fileReadFacts{kind: fileUnreadable, err: errors.Join(cancellation, err)}
	}
	if err != nil {
		return fileReadFacts{kind: fileUnreadable, err: err}
	}
	if !os.SameFile(info, actual) {
		return fileReadFacts{kind: fileUnreadable, err: errConfigurationChanged}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if cancellation := ctx.Err(); cancellation != nil {
		return fileReadFacts{kind: fileUnreadable, err: errors.Join(cancellation, err)}
	}
	if err != nil {
		return fileReadFacts{kind: fileUnreadable, err: err}
	}
	if len(data) > maxConfigBytes {
		return fileReadFacts{kind: fileTooLarge}
	}
	return fileReadFacts{kind: fileReadable, data: data}
}
