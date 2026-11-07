package startupinfra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/startupinfra/rules"
)

// migrationBackupSuffix names the v1 copy next to the target: config.yaml.v1.bak.
const migrationBackupSuffix = ".v1.bak"

// migrationEffects are the filesystem effects of PublishMigration. Tests
// substitute them to inject failures; production uses defaultMigrationEffects.
type migrationEffects struct {
	prepareTemp   func(directory string, data []byte) (string, error)
	rename        func(from, to string) error
	syncDirectory func(directory string) error
	source        configurationFileSource
}

func defaultMigrationEffects() migrationEffects {
	return migrationEffects{prepareConfigurationTemp, os.Rename, syncDirectory, configurationFileSource{os.Lstat, openConfigurationFile}}
}

// PublishMigration replaces a valid v1 file with v2 (spec 009, "Замяна на v1
// файла"). This is the single, explicit exception to the spec 005 rule that
// publication never renames over an existing file; initial creation still uses
// Publish's create-if-absent link. Steps:
//
//  1. backup: hard-link the target to config.yaml.v1.bak, create-if-absent. An
//     existing backup is never overwritten: if it is a link to the read file
//     (same identity and bytes; an earlier or concurrent attempt) migration
//     proceeds, otherwise it stops with startup.ErrMigrationBackupConflict.
//     The backup is kept on every path.
//  2. prepare: a complete, synced, closed temp file in the same directory.
//  3. verify: re-read the target; unless it is the same file with the same
//     bytes as source, return MigrationSourceChanged without writing.
//  4. replace: rename the temp over the target, then sync the directory.
//
// The window between 3 and 4 is accepted; an edit made in it survives in the
// backup. A failure before 4 removes only this operation's temp file and
// leaves the target untouched. A durability failure after 4 returns
// MigrationReplaced with the error. On Windows os.Rename replaces via
// MoveFileEx and directory sync is a no-op (see sync_windows.go).
func (Files) PublishMigration(ctx context.Context, path string, source startup.ConfigSource, next configuration.Config) (startup.MigrationKind, error) {
	return publishMigration(ctx, path, source, next, defaultMigrationEffects())
}

func publishMigration(ctx context.Context, path string, source startup.ConfigSource, next configuration.Config, effects migrationEffects) (kind startup.MigrationKind, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if source.Identity == nil {
		return "", errors.New("migration source identity is unknown")
	}
	data, err := configuration.Encode(next)
	if err != nil {
		return "", err
	}
	if err = backupConfiguration(ctx, path, source, effects.source); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	name, err := effects.prepareTemp(directory, data)
	if err != nil {
		return "", err
	}
	renamed := false
	defer func() {
		if !renamed {
			err = errors.Join(err, removeConfigurationTemp(name))
		}
	}()
	decision, err := verifyMigrationSource(ctx, path, source, effects.source)
	if err != nil {
		return "", err
	}
	if decision.Kind == rules.SourceChanged {
		return startup.MigrationSourceChanged, nil
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = effects.rename(name, path); err != nil {
		return "", fmt.Errorf("replace configuration: %w", err)
	}
	renamed = true
	if err = effects.syncDirectory(directory); err != nil {
		return startup.MigrationReplaced, err
	}
	return startup.MigrationReplaced, nil
}

// backupConfiguration performs step 1. A new link needs no comparison: it is
// the target's current file, and step 3 rejects it if that file is no longer
// the one that was read.
func backupConfiguration(ctx context.Context, path string, source startup.ConfigSource, files configurationFileSource) error {
	backup := path + migrationBackupSuffix
	err := os.Link(path, backup)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create configuration backup: %w", err)
	}
	existing := readConfigurationFile(ctx, backup, files)
	if cancellation := ctx.Err(); cancellation != nil {
		return errors.Join(cancellation, existing.err)
	}
	readable := existing.kind == fileReadable
	decision := rules.DecideExistingBackup(rules.BackupFacts{
		Readable:  readable,
		SameFile:  readable && os.SameFile(source.Identity, existing.info),
		SameBytes: readable && bytes.Equal(existing.data, source.Data),
	})
	if decision.Kind == rules.BackupConflict {
		return errors.Join(startup.ErrMigrationBackupConflict, existing.err)
	}
	return nil
}

// verifyMigrationSource performs step 3 and returns a technical error only for
// cancellation or an unreadable target; a changed target is a decision.
func verifyMigrationSource(ctx context.Context, path string, source startup.ConfigSource, files configurationFileSource) (rules.ReplacementDecision, error) {
	current := readConfigurationFile(ctx, path, files)
	if cancellation := ctx.Err(); cancellation != nil {
		return rules.ReplacementDecision{}, errors.Join(cancellation, current.err)
	}
	readable := current.kind == fileReadable
	decision := rules.DecideReplacement(rules.ReplacementFacts{
		Absent:    current.kind == fileAbsent,
		Readable:  readable,
		SameFile:  readable && os.SameFile(source.Identity, current.info),
		SameBytes: readable && bytes.Equal(current.data, source.Data),
	})
	if decision.Kind == rules.SourceUnreadable {
		return decision, errors.Join(errors.New(decision.Reason), current.err)
	}
	return decision, nil
}
