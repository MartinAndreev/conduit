package projectinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogPathIsGlobalAndReadOnly(t *testing.T) {
	home := t.TempDir()
	calls := 0
	path, err := ResolveCatalogPath(context.Background(), func() (string, error) { calls++; return home, nil })
	if err != nil || path != filepath.Join(home, ".conduit", "catalog.db") || calls != 1 {
		t.Fatal(path, err, calls)
	}
	if _, err := os.Stat(filepath.Join(home, ".conduit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("path resolution created storage", err)
	}
	for _, bad := range []string{"", "relative", filepath.Join(home, "invalid\x00home")} {
		path, err := ResolveCatalogPath(context.Background(), func() (string, error) { return bad, nil })
		if path != "" || !errors.Is(err, ErrHomeUnavailable) {
			t.Fatal(path, err)
		}
	}
}

func TestCatalogHomeFailureAndCancellation(t *testing.T) {
	cause := errors.New("home lookup failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	path, err := ResolveCatalogPath(ctx, func() (string, error) { calls++; return "", nil })
	if path != "" || !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(path, err, calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	path, err = ResolveCatalogPath(ctx, func() (string, error) { cancel(); return "", cause })
	if path != "" || !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal(path, err)
	}
	path, err = ResolveCatalogPath(context.Background(), func() (string, error) { return "", cause })
	if path != "" || !errors.Is(err, cause) {
		t.Fatal(path, err)
	}
}
