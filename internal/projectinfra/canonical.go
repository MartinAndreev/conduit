package projectinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// realCasePath uses directory-entry spelling only after proving identity. This
// handles case aliases on insensitive filesystems without merging distinct
// directories on a sensitive filesystem. No approximate spelling is returned.
func realCasePath(ctx context.Context, path string, source directorySource) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrInvalidPath
	}
	volume := filepath.VolumeName(path)
	root := volume + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		// Drive letters and UNC server/share roots have Windows case-insensitive
		// identity. Component spelling below remains the actual directory spelling.
		root = strings.ToUpper(volume) + string(filepath.Separator)
	}
	remainder := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	if remainder == "" {
		return root, ctx.Err()
	}
	canonical := root
	for _, component := range strings.Split(remainder, string(filepath.Separator)) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		next := filepath.Join(canonical, component)
		target, err := source.stat(next)
		if err = errors.Join(err, ctx.Err()); err != nil {
			return "", err
		}
		entries, err := source.readDir(canonical)
		if err = errors.Join(err, ctx.Err()); err != nil {
			return "", err
		}
		actual, err := directoryEntryName(ctx, entries, component, target)
		if err != nil {
			return "", err
		}
		canonical = filepath.Join(canonical, actual)
	}
	return canonical, nil
}

func directoryEntryName(ctx context.Context, entries []os.DirEntry, component string, target os.FileInfo) (string, error) {
	// Exact spelling comes first, so unrelated siblings cannot introduce errors
	// when the requested entry is directly available.
	for _, entry := range entries {
		if entry.Name() != component {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := entry.Info()
		if err = errors.Join(err, ctx.Err()); err != nil {
			return "", err
		}
		if entry.Type()&os.ModeSymlink == 0 && os.SameFile(target, info) {
			return entry.Name(), nil
		}
		return "", ErrDirectoryChanged
	}
	// Equal identity also handles filesystem Unicode normalization aliases; a
	// text case-fold alone would not prove the directory is the same one.
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := entry.Info()
		if err = errors.Join(err, ctx.Err()); err != nil {
			return "", err
		}
		if os.SameFile(target, info) {
			return entry.Name(), nil
		}
	}
	return "", ErrDirectoryChanged
}
