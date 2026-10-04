package projectinfra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrHomeUnavailable = errors.New("an absolute user home directory is required")

// ResolveCatalogPath resolves global storage without creating it. Supplying a
// home resolver allows the composition root to isolate tests from user data.
// A failed resolver never falls back to the process working directory.
func ResolveCatalogPath(ctx context.Context, home func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if home == nil {
		home = os.UserHomeDir
	}
	directory, err := home()
	if err = errors.Join(err, ctx.Err()); err != nil {
		return "", fmt.Errorf("resolve catalog home: %w", err)
	}
	if !filepath.IsAbs(directory) || strings.ContainsRune(directory, 0) {
		return "", ErrHomeUnavailable
	}
	return filepath.Join(directory, ".conduit", "catalog.db"), nil
}
