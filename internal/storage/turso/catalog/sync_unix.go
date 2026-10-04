//go:build !windows

package catalog

import (
	"errors"
	"fmt"
	"os"
)

func syncCatalogDirectory(path string) (err error) {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open catalog directory for sync: %w", err)
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync catalog directory: %w", err)
	}
	return nil
}
