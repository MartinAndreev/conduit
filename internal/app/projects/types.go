// Package projects composes catalog reads and independent persistence effects.
package projects

import (
	"context"
	"errors"
	domain "github.com/MartinAndreev/conduit/internal/projects"
)

// Directories observes only directories, never writes project contents.
type Directories interface {
	Resolve(context.Context, string, string) (domain.Directory, error)
	Inspect(context.Context, string) (domain.Directory, error)
}

// Repository reads must not create storage. Insert atomically enforces canonical
// path uniqueness and returns the existing project when another writer wins.
// Persisted remains true when a completed write is followed by a durability or
// cancellation error; callers must retain the returned project in that case.
type Repository interface {
	Read(context.Context) (Snapshot, error)
	Insert(context.Context, domain.Project) (InsertResult, error)
	SetLastOpened(context.Context, domain.ApplicationID, domain.ID) (WriteResult, error)
}
type Snapshot struct {
	Projects   []domain.Project
	LastOpened map[domain.ApplicationID]domain.ID
}
type InsertResult struct {
	Project   domain.Project
	Existing  bool
	Persisted bool
}
type WriteResult struct{ Persisted bool }

var (
	ErrInvalidApplication = errors.New("invalid application ID")
	ErrUnknownProject     = errors.New("unknown project")
	ErrDanglingPreference = errors.New("last-opened project is absent from catalog")
	ErrCorruptCatalog     = errors.New("corrupt catalog")
	ErrFutureCatalog      = errors.New("unsupported catalog version")
)

type CatalogEntry struct {
	Project   domain.Project
	Directory domain.Directory
}
type Catalog struct{ Entries []CatalogEntry }
type RegistrationOutcome struct {
	Kind      domain.RegistrationKind
	Project   domain.Project
	Directory domain.Directory
	Persisted bool
	Reason    string
}
type RestoreKind string

const (
	RestoreNone        RestoreKind = "none"
	RestoreReady       RestoreKind = "ready"
	RestoreUnavailable RestoreKind = "unavailable"
	RestoreDangling    RestoreKind = "dangling"
)

type RestoreOutcome struct {
	Kind      RestoreKind
	Project   domain.Project
	Directory domain.Directory
}
type OpenedOutcome struct {
	Project   domain.Project
	Directory domain.Directory
	Persisted bool
}
type RegistrationAndOpenOutcome struct {
	Registration RegistrationOutcome
	Opened       OpenedOutcome
}
type Service struct {
	Repository  Repository
	Directories Directories
}
