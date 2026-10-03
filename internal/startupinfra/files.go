// Package startupinfra implements local startup effects. It never opens agent sessions.
package startupinfra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
)

const maxConfigBytes = 1024 * 1024

// Files resolves the real user home by default. Home is injectable for isolated tests.
type Files struct{ Home func() (string, error) }

func (f Files) Resolve(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	home := f.Home
	if home == nil {
		home = os.UserHomeDir
	}
	directory, err := home()
	if err != nil {
		return "", err
	}
	if directory == "" || !filepath.IsAbs(directory) {
		return "", errors.New("user home is unavailable")
	}
	return filepath.Join(directory, ".conduit", "config.yaml"), nil
}
func (Files) Read(ctx context.Context, path string) startup.ReadResult {
	facts := readConfigurationFile(ctx, path, configurationFileSource{os.Lstat, openConfigurationFile})
	switch facts.kind {
	case fileAbsent:
		return startup.ReadResult{Kind: startup.Absent}
	case fileUnreadable:
		return startup.ReadResult{Kind: startup.Unreadable, Err: facts.err}
	case fileTooLarge:
		return startup.ReadResult{Kind: startup.Invalid, Err: configuration.ErrInvalid}
	}

	config, err := configuration.Decode(facts.data)
	if errors.Is(err, configuration.ErrFutureSchema) {
		return startup.ReadResult{Kind: startup.FutureSchema, Err: err}
	}
	if err != nil {
		return startup.ReadResult{Kind: startup.Invalid, Err: err}
	}
	if err := ctx.Err(); err != nil {
		return startup.ReadResult{Kind: startup.Unreadable, Err: err}
	}
	return startup.ReadResult{Kind: startup.Valid, Config: config}
}

// Publish syncs and closes a private same-directory temp file, then creates a
// hard link at the destination. Link is atomic and cannot replace any existing
// path (including a symlink). No rename/check-then-write fallback is permitted.
// A successful link means Published even when later directory sync fails.
func (Files) Publish(ctx context.Context, path string, c configuration.Config) (kind startup.PublishKind, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	data, err := configuration.Encode(c)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	name, err := prepareConfigurationTemp(directory, data)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, removeConfigurationTemp(name)) }()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Link(name, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return startup.AlreadyExists, nil
		}
		return "", fmt.Errorf("atomic create-if-absent publication unavailable: %w", err)
	}
	if err = syncDirectory(directory); err != nil {
		return startup.Published, err
	}
	return startup.Published, nil
}
