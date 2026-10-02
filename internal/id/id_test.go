package id

import (
	"errors"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestNewWithReader(t *testing.T) {
	got, err := newWithReader("turn", strings.NewReader("abcdefghijkl"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "turn-6162636465666768696a6b6c" {
		t.Fatalf("unexpected ID %q", got)
	}
}

func TestNewWithReaderDoesNotFallback(t *testing.T) {
	got, err := newWithReader("turn", failingReader{})
	if err == nil || got != "" {
		t.Fatalf("expected empty ID and error, got %q, %v", got, err)
	}
}
