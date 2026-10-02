// Package id creates opaque adapter-local identifiers.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// New returns a cryptographically random identifier. It never substitutes a
// predictable fallback when the operating system random source fails.
func New(prefix string) (string, error) {
	return newWithReader(prefix, rand.Reader)
}

func newWithReader(prefix string, source io.Reader) (string, error) {
	var value [12]byte
	if _, err := io.ReadFull(source, value[:]); err != nil {
		return "", fmt.Errorf("generate %s identifier: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(value[:]), nil
}
