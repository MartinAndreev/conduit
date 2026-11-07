package startupinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
)

// fixtureExecutable is absolute on every OS (os.TempDir is absolute) and is
// never created. strconv.Quote yields a valid YAML double-quoted scalar, so
// Windows backslashes are escaped.
var fixtureExecutable = filepath.Join(os.TempDir(), "conduit-fixture", "codex")

var v1Data = "schema_version: 1\nharnesses:\n  codex: {executable: " + strconv.Quote(fixtureExecutable) + "}\n  opencode: {}\nassignments: {}\n"

// writeV1 creates a v1 target and returns the startup read of it.
func writeV1(t *testing.T) (string, startup.ReadResult) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(v1Data), 0600); err != nil {
		t.Fatal(err)
	}
	read := (Files{}).Read(context.Background(), path)
	if read.Kind != startup.Valid || read.Config.SchemaVersion != configuration.SchemaV1 {
		t.Fatal(read)
	}
	return path, read
}

func migrated(t *testing.T, read startup.ReadResult) configuration.Config {
	t.Helper()
	observations := map[configuration.HarnessID]configuration.Observation{}
	for _, id := range configuration.MissingHarnesses(read.Config) {
		observations[id] = configuration.Observation{Kind: configuration.Missing}
	}
	next, err := configuration.MigrateV1(read.Config, observations)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func directoryNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
	}
}

func TestPublishMigrationReplacesAndKeepsBackup(t *testing.T) {
	path, read := writeV1(t)
	kind, err := (Files{}).PublishMigration(context.Background(), path, read.Source, migrated(t, read))
	if err != nil || kind != startup.MigrationReplaced {
		t.Fatal(kind, err)
	}
	after := (Files{}).Read(context.Background(), path)
	if after.Kind != startup.Valid || after.Config.SchemaVersion != configuration.CurrentSchema || after.Config.Harnesses[configuration.Codex].Executable != fixtureExecutable {
		t.Fatal(after)
	}
	assertFile(t, path+migrationBackupSuffix, v1Data)
	if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml", "config.yaml.v1.bak"}) {
		t.Fatal(names)
	}
}

func TestPublishMigrationExistingBackup(t *testing.T) {
	t.Run("link to the read file proceeds and is kept", func(t *testing.T) {
		path, read := writeV1(t)
		backup := path + migrationBackupSuffix
		if err := os.Link(path, backup); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Stat(backup)
		kind, err := (Files{}).PublishMigration(context.Background(), path, read.Source, migrated(t, read))
		if err != nil || kind != startup.MigrationReplaced {
			t.Fatal(kind, err)
		}
		afterInfo, _ := os.Stat(backup)
		if !os.SameFile(before, afterInfo) {
			t.Fatal("backup replaced")
		}
		assertFile(t, backup, v1Data)
	})
	for name, data := range map[string]string{
		"byte-identical copy blocks": v1Data,
		"different bytes blocks":     "older backup",
	} {
		t.Run(name, func(t *testing.T) {
			path, read := writeV1(t)
			backup := path + migrationBackupSuffix
			if err := os.WriteFile(backup, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(backup)
			kind, err := (Files{}).PublishMigration(context.Background(), path, read.Source, migrated(t, read))
			if kind != "" || !errors.Is(err, startup.ErrMigrationBackupConflict) {
				t.Fatal(kind, err)
			}
			assertFile(t, path, v1Data)
			assertFile(t, backup, data)
			if afterInfo, _ := os.Stat(backup); !os.SameFile(before, afterInfo) {
				t.Fatal("backup replaced")
			}
			if names := directoryNames(t, path); len(names) != 2 {
				t.Fatal(names)
			}
		})
	}
}

func TestPublishMigrationDetectsConcurrentChange(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, path string){
		"bytes changed": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(v1Data+"# edited\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"same bytes, other file": func(t *testing.T, path string) {
			other := path + ".other"
			if err := os.WriteFile(other, []byte(v1Data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(other, path); err != nil {
				t.Fatal(err)
			}
		},
		"deleted": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path, read := writeV1(t)
			effects := defaultMigrationEffects()
			// Change the target after backup and temp preparation, right
			// before the verification re-read.
			effects.prepareTemp = func(directory string, data []byte) (string, error) {
				name, err := prepareConfigurationTemp(directory, data)
				change(t, path)
				return name, err
			}
			kind, err := publishMigration(context.Background(), path, read.Source, migrated(t, read), effects)
			if err != nil || kind != startup.MigrationSourceChanged {
				t.Fatal(kind, err)
			}
			after, statErr := os.ReadFile(path)
			if name == "deleted" {
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal("target recreated")
				}
			} else if want := map[bool]string{true: v1Data + "# edited\n", false: v1Data}[name == "bytes changed"]; string(after) != want {
				t.Fatal("changed target overwritten", string(after))
			}
			for _, entry := range directoryNames(t, path) {
				if entry != "config.yaml" && entry != "config.yaml.v1.bak" {
					t.Fatal("temp left", entry)
				}
			}
		})
	}
}

// failingTemp wraps the real temp file so that the real close always happens.
type failingTemp struct {
	file                        *os.File
	writeErr, syncErr, closeErr error
}

func (f failingTemp) Write(b []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.file.Write(b)
}
func (f failingTemp) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.file.Sync()
}
func (f failingTemp) Close() error { return errors.Join(f.file.Close(), f.closeErr) }

func TestPublishMigrationFailuresBeforeRenameKeepOriginal(t *testing.T) {
	cause := errors.New("injected failure")
	for name, inject := range map[string]func(*migrationEffects){
		"write": func(e *migrationEffects) {
			e.prepareTemp = func(d string, data []byte) (string, error) {
				return prepareConfigurationTempWith(d, data, func(f *os.File) writableConfigurationTemp { return failingTemp{file: f, writeErr: cause} })
			}
		},
		"sync": func(e *migrationEffects) {
			e.prepareTemp = func(d string, data []byte) (string, error) {
				return prepareConfigurationTempWith(d, data, func(f *os.File) writableConfigurationTemp { return failingTemp{file: f, syncErr: cause} })
			}
		},
		"close": func(e *migrationEffects) {
			e.prepareTemp = func(d string, data []byte) (string, error) {
				return prepareConfigurationTempWith(d, data, func(f *os.File) writableConfigurationTemp { return failingTemp{file: f, closeErr: cause} })
			}
		},
		"rename": func(e *migrationEffects) {
			e.rename = func(string, string) error { return cause }
		},
	} {
		t.Run(name, func(t *testing.T) {
			path, read := writeV1(t)
			effects := defaultMigrationEffects()
			inject(&effects)
			kind, err := publishMigration(context.Background(), path, read.Source, migrated(t, read), effects)
			if kind != "" || !errors.Is(err, cause) {
				t.Fatal(kind, err)
			}
			assertFile(t, path, v1Data)
			if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml", "config.yaml.v1.bak"}) {
				t.Fatal("temp left or target lost", names)
			}
		})
	}
}

func TestPublishMigrationDurabilityFailureAfterRename(t *testing.T) {
	path, read := writeV1(t)
	cause := errors.New("directory sync failed")
	effects := defaultMigrationEffects()
	effects.syncDirectory = func(string) error { return cause }
	kind, err := publishMigration(context.Background(), path, read.Source, migrated(t, read), effects)
	if kind != startup.MigrationReplaced || !errors.Is(err, cause) {
		t.Fatal(kind, err)
	}
	if after := (Files{}).Read(context.Background(), path); after.Config.SchemaVersion != configuration.CurrentSchema {
		t.Fatal(after)
	}
	if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml", "config.yaml.v1.bak"}) {
		t.Fatal(names)
	}
}

func TestPublishMigrationCancellation(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		path, read := writeV1(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		kind, err := (Files{}).PublishMigration(ctx, path, read.Source, migrated(t, read))
		if kind != "" || !errors.Is(err, context.Canceled) {
			t.Fatal(kind, err)
		}
		assertFile(t, path, v1Data)
		if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml"}) {
			t.Fatal("effect after cancellation", names)
		}
	})
	t.Run("before rename", func(t *testing.T) {
		path, read := writeV1(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		renames := 0
		effects := defaultMigrationEffects()
		effects.prepareTemp = func(d string, data []byte) (string, error) {
			name, err := prepareConfigurationTemp(d, data)
			cancel()
			return name, err
		}
		effects.rename = func(from, to string) error { renames++; return os.Rename(from, to) }
		kind, err := publishMigration(ctx, path, read.Source, migrated(t, read), effects)
		if kind != "" || !errors.Is(err, context.Canceled) || renames != 0 {
			t.Fatal(kind, err, renames)
		}
		assertFile(t, path, v1Data)
		if names := directoryNames(t, path); !slices.Equal(names, []string{"config.yaml", "config.yaml.v1.bak"}) {
			t.Fatal("temp left", names)
		}
	})
}

// Real files end to end: a v1 file is migrated by Initialize, and a second
// run leaves the v2 file byte-for-byte unchanged.
func TestApplicationMigratesV1File(t *testing.T) {
	home := t.TempDir()
	f := Files{Home: func() (string, error) { return home, nil }}
	path, _ := f.Resolve(context.Background())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(v1Data), 0600); err != nil {
		t.Fatal(err)
	}
	s := startup.Service{Repository: f, Detector: localMissing{}, Migrator: f}
	first := s.Initialize(context.Background(), nil)
	if first.Snapshot.Status != startup.ReadyWithWarnings || !first.Published || first.Snapshot.Steps[2].Status != startup.Completed {
		t.Fatal(first)
	}
	assertFile(t, path+migrationBackupSuffix, v1Data)
	before, _ := os.ReadFile(path)
	second := s.Initialize(context.Background(), nil)
	after, _ := os.ReadFile(path)
	if second.Published || string(before) != string(after) || second.Snapshot.Steps[2].Status != startup.Skipped {
		t.Fatal(second)
	}
}
