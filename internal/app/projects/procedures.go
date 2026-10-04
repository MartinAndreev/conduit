package projects

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/MartinAndreev/conduit/internal/id"
	domain "github.com/MartinAndreev/conduit/internal/projects"
)

// ReadCatalog retains registrations with observed availability failures. It returns
// their causes both on entries and in a joined error; callers can render the full
// catalog while inspecting errors.Is/As. Cancellation stops further observations.
func (s Service) ReadCatalog(ctx context.Context) (Catalog, error) {
	snapshot, err := s.Repository.Read(ctx)
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog: %w", err)
	}
	sort.Slice(snapshot.Projects, func(i, j int) bool {
		a, b := snapshot.Projects[i], snapshot.Projects[j]
		if a.Name == b.Name {
			return a.CanonicalPath < b.CanonicalPath
		}
		return a.Name < b.Name
	})
	catalog := Catalog{Entries: make([]CatalogEntry, 0, len(snapshot.Projects))}
	var observations error
	for _, project := range snapshot.Projects {
		if err := ctx.Err(); err != nil {
			return catalog, errors.Join(observations, err)
		}
		directory, err := s.Directories.Inspect(ctx, project.CanonicalPath)
		// Availability failures belong to the entry. Keep the complete catalog and
		// also expose the original causes to callers through the aggregate error.
		if err != nil {
			observed := directory.Kind == domain.Missing || directory.Kind == domain.Unreadable || directory.Kind == domain.NotDirectory
			wrapped := fmt.Errorf("inspect project %s: %w", project.ID, err)
			if !observed || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return catalog, errors.Join(observations, wrapped, ctx.Err())
			}
			directory.Cause = errors.Join(directory.Cause, err)
			observations = errors.Join(observations, wrapped)
		}
		catalog.Entries = append(catalog.Entries, CatalogEntry{project, directory})
	}
	return catalog, observations
}

func (s Service) RegisterLocalProject(ctx context.Context, input, base string) (RegistrationOutcome, error) {
	directory, err := s.Directories.Resolve(ctx, input, base)
	if err != nil {
		return RegistrationOutcome{Kind: domain.RegistrationRejected, Directory: directory, Reason: "The directory could not be resolved."}, fmt.Errorf("resolve project directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return RegistrationOutcome{Directory: directory}, err
	}
	// Reject unavailable input before touching catalog storage.
	decision := domain.DecideRegistration(domain.RegistrationFacts{Directory: directory})
	if decision.Kind == domain.RegistrationRejected {
		return RegistrationOutcome{Kind: decision.Kind, Directory: directory, Reason: decision.Reason}, nil
	}
	snapshot, err := s.Repository.Read(ctx)
	if err != nil {
		return RegistrationOutcome{Directory: directory}, fmt.Errorf("read registration facts: %w", err)
	}
	existing := findPath(snapshot.Projects, directory.CanonicalPath)
	decision = domain.DecideRegistration(domain.RegistrationFacts{Directory: directory, Existing: existing})
	if decision.Kind == domain.RegistrationExisting {
		return RegistrationOutcome{Kind: decision.Kind, Project: *existing, Directory: directory, Persisted: true, Reason: decision.Reason}, nil
	}
	if err := ctx.Err(); err != nil {
		return RegistrationOutcome{Directory: directory}, err
	}
	rawID, err := id.New("project")
	if err != nil {
		return RegistrationOutcome{Directory: directory}, err
	}
	project := domain.Project{ID: domain.ID(rawID), Name: directory.Name, CanonicalPath: directory.CanonicalPath}
	if err := ctx.Err(); err != nil {
		return RegistrationOutcome{Directory: directory}, err
	}
	inserted, err := s.Repository.Insert(ctx, project)
	kind := domain.RegistrationAllowed
	if inserted.Existing {
		kind = domain.RegistrationExisting
	}
	outcome := RegistrationOutcome{Kind: kind, Project: inserted.Project, Directory: directory, Persisted: inserted.Persisted, Reason: decision.Reason}
	if err != nil {
		return outcome, fmt.Errorf("persist registration: %w", err)
	}
	return outcome, nil
}

func (s Service) RestoreLastProject(ctx context.Context, application domain.ApplicationID) (RestoreOutcome, error) {
	if !domain.ValidApplication(application) {
		return RestoreOutcome{}, ErrInvalidApplication
	}
	snapshot, err := s.Repository.Read(ctx)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("read last-opened project: %w", err)
	}
	projectID := snapshot.LastOpened[application]
	if projectID == "" {
		return RestoreOutcome{Kind: RestoreNone}, nil
	}
	project := findID(snapshot.Projects, projectID)
	if project == nil {
		return RestoreOutcome{Kind: RestoreDangling}, ErrDanglingPreference
	}
	if err := ctx.Err(); err != nil {
		return RestoreOutcome{Project: *project}, err
	}
	directory, err := s.Directories.Inspect(ctx, project.CanonicalPath)
	outcome := RestoreOutcome{Kind: RestoreUnavailable, Project: *project, Directory: directory}
	if err != nil {
		return outcome, fmt.Errorf("inspect last-opened project: %w", err)
	}
	if domain.DecideOpening(directory).Kind == domain.OpeningAllowed {
		outcome.Kind = RestoreReady
	}
	return outcome, nil
}

// RecordOpenedProject revalidates availability before the independent preference
// effect. It does not open a screen or execute anything in the project.
func (s Service) RecordOpenedProject(ctx context.Context, application domain.ApplicationID, projectID domain.ID) (OpenedOutcome, error) {
	if !domain.ValidApplication(application) {
		return OpenedOutcome{}, ErrInvalidApplication
	}
	snapshot, err := s.Repository.Read(ctx)
	if err != nil {
		return OpenedOutcome{}, fmt.Errorf("read opened project: %w", err)
	}
	project := findID(snapshot.Projects, projectID)
	if project == nil {
		return OpenedOutcome{}, ErrUnknownProject
	}
	if err := ctx.Err(); err != nil {
		return OpenedOutcome{Project: *project}, err
	}
	directory, err := s.Directories.Inspect(ctx, project.CanonicalPath)
	outcome := OpenedOutcome{Project: *project, Directory: directory}
	if err != nil {
		return outcome, fmt.Errorf("inspect opened project: %w", err)
	}
	if domain.DecideOpening(directory).Kind == domain.OpeningUnavailable {
		return outcome, nil
	}
	if err := ctx.Err(); err != nil {
		return outcome, err
	}
	written, err := s.Repository.SetLastOpened(ctx, application, projectID)
	outcome.Persisted = written.Persisted
	if err != nil {
		return outcome, fmt.Errorf("persist last-opened project: %w", err)
	}
	return outcome, nil
}

// RegisterAndRecordOpened exposes both effects. Retrying re-reads the catalog;
// preference failure never removes a successful registration.
func (s Service) RegisterAndRecordOpened(ctx context.Context, application domain.ApplicationID, input, base string) (RegistrationAndOpenOutcome, error) {
	if !domain.ValidApplication(application) {
		return RegistrationAndOpenOutcome{}, ErrInvalidApplication
	}
	registration, err := s.RegisterLocalProject(ctx, input, base)
	outcome := RegistrationAndOpenOutcome{Registration: registration}
	if err != nil || registration.Kind == domain.RegistrationRejected {
		return outcome, err
	}
	if err := ctx.Err(); err != nil {
		return outcome, err
	}
	outcome.Opened, err = s.RecordOpenedProject(ctx, application, registration.Project.ID)
	return outcome, err
}

func findPath(projects []domain.Project, path string) *domain.Project {
	for _, project := range projects {
		if project.CanonicalPath == path {
			return &project
		}
	}
	return nil
}
func findID(projects []domain.Project, id domain.ID) *domain.Project {
	for _, project := range projects {
		if project.ID == id {
			return &project
		}
	}
	return nil
}
