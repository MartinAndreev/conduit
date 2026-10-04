// Package projectinfra observes local directory availability and identity.
// It never creates or changes files in a project directory.
package projectinfra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domain "github.com/MartinAndreev/conduit/internal/projects"
)

var ErrInvalidPath = errors.New("project path must be nonempty and relative paths require an absolute base")
var ErrNotDirectory = errors.New("project path is not a directory")
var ErrDirectoryChanged = errors.New("directory identity changed during canonicalization")

// Directories implements the application directory query port. The zero value
// uses the OS; the private source permits deterministic failures in local tests.
type Directories struct{ source *directorySource }

type directorySource struct {
	stat         func(string) (os.FileInfo, error)
	readDir      func(string) ([]os.DirEntry, error)
	evalSymlinks func(string) (string, error)
}

func (d Directories) filesystem() directorySource {
	if d.source != nil {
		return *d.source
	}
	return directorySource{stat: os.Stat, readDir: os.ReadDir, evalSymlinks: filepath.EvalSymlinks}
}

func (d Directories) Resolve(ctx context.Context, input, base string) (domain.Directory, error) {
	if err := ctx.Err(); err != nil {
		return failedDirectory(input, domain.Unreadable, err)
	}
	if strings.TrimSpace(input) == "" || strings.ContainsRune(input, 0) {
		return failedDirectory(input, domain.InvalidPath, ErrInvalidPath)
	}
	path := input
	if !filepath.IsAbs(path) {
		// A drive-relative or rooted Windows path is not relative to the supplied
		// base, and would reintroduce implicit OS current-directory state.
		if !filepath.IsAbs(base) || filepath.VolumeName(path) != "" || strings.HasPrefix(path, string(filepath.Separator)) {
			return failedDirectory(input, domain.InvalidPath, ErrInvalidPath)
		}
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	source := d.filesystem()
	directory, err := inspectDirectory(ctx, path, source)
	if err != nil {
		return directory, err
	}
	if err := ctx.Err(); err != nil {
		return failedDirectory(path, domain.Unreadable, err)
	}
	resolved, err := source.evalSymlinks(path)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return failedDirectory(path, domain.CanonicalizationFailed, fmt.Errorf("resolve project symlinks: %w", err))
	}
	canonical, err := realCasePath(ctx, filepath.Clean(resolved), source)
	if err != nil {
		return failedDirectory(path, domain.CanonicalizationFailed, fmt.Errorf("resolve directory identity: %w", err))
	}
	// Canonicalization can observe a replacement after the initial inspection.
	// Validate the final target again; this remains an observation, not a lock
	// against filesystem changes after the query returns.
	return inspectDirectory(ctx, canonical, source)
}

// Inspect checks the saved location without searching for moved directories or
// silently replacing the stored canonical path with a new target.
func (d Directories) Inspect(ctx context.Context, canonical string) (domain.Directory, error) {
	if err := ctx.Err(); err != nil {
		return failedDirectory(canonical, domain.Unreadable, err)
	}
	if !filepath.IsAbs(canonical) || strings.ContainsRune(canonical, 0) {
		return failedDirectory(canonical, domain.InvalidPath, ErrInvalidPath)
	}
	return inspectDirectory(ctx, canonical, d.filesystem())
}

func inspectDirectory(ctx context.Context, path string, source directorySource) (domain.Directory, error) {
	if err := ctx.Err(); err != nil {
		return failedDirectory(path, domain.Unreadable, err)
	}
	info, err := source.stat(path)
	if cancellation := ctx.Err(); cancellation != nil {
		return failedDirectory(path, domain.Unreadable, errors.Join(cancellation, err))
	}
	if err != nil {
		kind := domain.Unreadable
		if errors.Is(err, os.ErrNotExist) {
			kind = domain.Missing
		}
		return failedDirectory(path, kind, err)
	}
	if !info.IsDir() {
		return failedDirectory(path, domain.NotDirectory, ErrNotDirectory)
	}
	// Stat alone can succeed for a directory whose entries cannot be read.
	if err := ctx.Err(); err != nil {
		return failedDirectory(path, domain.Unreadable, err)
	}
	_, err = source.readDir(path)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return failedDirectory(path, domain.Unreadable, err)
	}
	return domain.Directory{CanonicalPath: path, Name: filepath.Base(path), Kind: domain.Available}, nil
}

func failedDirectory(path string, kind domain.AvailabilityKind, err error) (domain.Directory, error) {
	return domain.Directory{CanonicalPath: path, Name: filepath.Base(path), Kind: kind, Cause: err}, err
}
