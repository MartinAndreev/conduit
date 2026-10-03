package startupinfra

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
)

func initialConfig() configuration.Config {
	return configuration.Initial(map[configuration.HarnessID]configuration.Observation{configuration.Codex: {Kind: configuration.Missing}, configuration.OpenCode: {Kind: configuration.Missing}})
}
func TestHomeResolutionNoFallback(t *testing.T) {
	home := t.TempDir()
	files := Files{Home: func() (string, error) { return home, nil }}
	path, err := files.Resolve(context.Background())
	if err != nil || path != filepath.Join(home, ".conduit", "config.yaml") {
		t.Fatal(path, err)
	}
	if _, err = os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("resolve created directory")
	}
	for _, h := range []string{"", "relative"} {
		_, err = (Files{Home: func() (string, error) { return h, nil }}).Resolve(context.Background())
		if err == nil {
			t.Fatal("accepted unavailable home")
		}
	}
	_, err = (Files{Home: func() (string, error) { return "", errors.New("home failed") }}).Resolve(context.Background())
	if err == nil {
		t.Fatal("missing error")
	}
}
func TestPublishReadAndPreserveBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".conduit", "config.yaml")
	f := Files{}
	if r := f.Read(context.Background(), path); r.Kind != startup.Absent {
		t.Fatal(r)
	}
	kind, err := f.Publish(context.Background(), path, initialConfig())
	if err != nil || kind != startup.Published {
		t.Fatal(kind, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before = append([]byte("# personal comment\n"), before...)
	if err = os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	kind, err = f.Publish(context.Background(), path, initialConfig())
	if err != nil || kind != startup.AlreadyExists {
		t.Fatal(kind, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing bytes changed")
	}
	if read := f.Read(context.Background(), path); read.Kind != startup.Valid {
		t.Fatal(read)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temp leaked", entries)
	}
}
func TestConcurrentPublicationOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".conduit", "config.yaml")
	var wg sync.WaitGroup
	results := make(chan startup.PublishKind, 16)
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
			results <- kind
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	winners := 0
	for kind := range results {
		if kind == startup.Published {
			winners++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal(winners)
	}
	if r := (Files{}).Read(context.Background(), path); r.Kind != startup.Valid {
		t.Fatal(r)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal(entries)
	}
}
func TestInvalidUnreadableAndSymlinkPreserved(t *testing.T) {
	for _, tc := range []struct {
		data  string
		kind  startup.ReadKind
		cause error
	}{
		{"schema_version: 2\nharnesses: {codex: {}, opencode: {}}\nassignments: {}\n", startup.FutureSchema, configuration.ErrFutureSchema},
		{"schema_version: broken\n", startup.Invalid, configuration.ErrInvalid},
		{"schema_version: 1\nschema_version: 1\n", startup.Invalid, configuration.ErrInvalid},
	} {
		data := tc.data
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		r := (Files{}).Read(context.Background(), path)
		if r.Kind != tc.kind || !errors.Is(r.Err, tc.cause) {
			t.Fatal(r)
		}
		kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
		if err != nil || kind != startup.AlreadyExists {
			t.Fatal(kind, err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != data {
			t.Fatal("replaced bad file")
		}
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	path := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip("symlink unavailable", err)
	}
	if r := (Files{}).Read(context.Background(), path); r.Kind != startup.Unreadable {
		t.Fatal(r)
	}
	kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
	if err != nil || kind != startup.AlreadyExists {
		t.Fatal(kind, err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal("symlink target changed")
	}
}
func TestPublicationFailureLeavesNoPartialFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "blocked")
	if err := os.WriteFile(parent, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "config.yaml")
	kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
	if err == nil || kind == startup.Published {
		t.Fatal(kind, err)
	}
	data, _ := os.ReadFile(parent)
	if string(data) != "unrelated" {
		t.Fatal("unrelated file altered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path = filepath.Join(root, "absent", "config.yaml")
	if _, err = (Files{}).Publish(ctx, path, initialConfig()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("effect after cancel")
	}
}

func TestUnreadablePermissionsPreserveOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data, _ := configuration.Encode(initialConfig())
	if err := os.WriteFile(path, data, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("process can bypass file permission restriction")
	}
	r := (Files{}).Read(context.Background(), path)
	if r.Kind != startup.Unreadable {
		t.Fatal(r)
	}
	kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
	if err != nil || kind != startup.AlreadyExists {
		t.Fatal(kind, err)
	}
}
func TestReadOnlyDirectoryRejectsPublicationAndPreservesOtherFiles(t *testing.T) {
	directory := t.TempDir()
	sentinel := filepath.Join(directory, "unrelated")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	path := filepath.Join(directory, "config.yaml")
	kind, err := (Files{}).Publish(context.Background(), path, initialConfig())
	if err == nil {
		t.Skip("process can bypass directory permission restriction")
	}
	if kind == startup.Published {
		t.Fatal("false success")
	}
	if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial target", err)
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 1 || entries[0].Name() != "unrelated" {
		t.Fatal(entries)
	}
}
