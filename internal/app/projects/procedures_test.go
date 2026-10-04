package projects

import (
	"context"
	"errors"
	"testing"

	domain "github.com/MartinAndreev/conduit/internal/projects"
)

type fakeRepository struct {
	snapshot                         Snapshot
	readErr, errorInsert, errorWrite error
	insertCalls, writeCalls          int
	afterRead                        func()
	persistedOnError                 bool
}

func (f *fakeRepository) Read(context.Context) (Snapshot, error) {
	if f.afterRead != nil {
		f.afterRead()
	}
	return Snapshot{Projects: append([]domain.Project(nil), f.snapshot.Projects...), LastOpened: f.snapshot.LastOpened}, f.readErr
}
func (f *fakeRepository) Insert(_ context.Context, p domain.Project) (InsertResult, error) {
	f.insertCalls++
	if f.errorInsert != nil {
		if f.persistedOnError {
			f.snapshot.Projects = append(f.snapshot.Projects, p)
		}
		return InsertResult{Project: p, Persisted: f.persistedOnError}, f.errorInsert
	}
	for _, existing := range f.snapshot.Projects {
		if existing.CanonicalPath == p.CanonicalPath {
			return InsertResult{Project: existing, Existing: true, Persisted: true}, nil
		}
	}
	f.snapshot.Projects = append(f.snapshot.Projects, p)
	return InsertResult{Project: p, Persisted: true}, nil
}
func (f *fakeRepository) SetLastOpened(_ context.Context, a domain.ApplicationID, id domain.ID) (WriteResult, error) {
	f.writeCalls++
	if f.errorWrite != nil {
		return WriteResult{}, f.errorWrite
	}
	if f.snapshot.LastOpened == nil {
		f.snapshot.LastOpened = map[domain.ApplicationID]domain.ID{}
	}
	f.snapshot.LastOpened[a] = id
	return WriteResult{Persisted: true}, nil
}

type fakeDirectories struct {
	result    domain.Directory
	err       error
	calls     int
	afterRead func()
}

func (f *fakeDirectories) Resolve(context.Context, string, string) (domain.Directory, error) {
	f.calls++
	if f.afterRead != nil {
		f.afterRead()
	}
	return f.result, f.err
}
func (f *fakeDirectories) Inspect(context.Context, string) (domain.Directory, error) {
	f.calls++
	if f.afterRead != nil {
		f.afterRead()
	}
	return f.result, f.err
}
func fixture() (Service, *fakeRepository, *fakeDirectories) {
	r := &fakeRepository{}
	d := &fakeDirectories{result: domain.Directory{Name: "project", CanonicalPath: "/project", Kind: domain.Available}}
	return Service{r, d}, r, d
}
func TestRegistrationAndRetry(t *testing.T) {
	s, r, _ := fixture()
	first, err := s.RegisterLocalProject(t.Context(), "input", "/")
	if err != nil || !first.Persisted || first.Project.ID == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := s.RegisterLocalProject(t.Context(), "alias", "/")
	if err != nil || second.Kind != domain.RegistrationExisting || second.Project.ID != first.Project.ID || r.insertCalls != 1 {
		t.Fatalf("duplicate: %+v %v", second, err)
	}
}
func TestUnavailableRegistrationPreservesCause(t *testing.T) {
	for _, kind := range []domain.AvailabilityKind{domain.InvalidPath, domain.NotDirectory, domain.Missing, domain.Unreadable, domain.CanonicalizationFailed} {
		t.Run(string(kind), func(t *testing.T) {
			s, r, d := fixture()
			cause := errors.New("filesystem cause")
			d.result.Kind = kind
			d.result.Cause = cause
			d.err = cause
			result, err := s.RegisterLocalProject(t.Context(), "input", "/")
			if result.Kind != domain.RegistrationRejected || !errors.Is(err, cause) || r.insertCalls != 0 {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestReadEmptyAndSort(t *testing.T) {
	s, r, d := fixture()
	c, err := s.ReadCatalog(t.Context())
	if err != nil || len(c.Entries) != 0 || d.calls != 0 || r.insertCalls != 0 {
		t.Fatalf("empty: %+v %v", c, err)
	}
	r.snapshot.Projects = []domain.Project{{ID: "3", Name: "z", CanonicalPath: "/z"}, {ID: "2", Name: "a", CanonicalPath: "/b"}, {ID: "1", Name: "a", CanonicalPath: "/a"}}
	c, err = s.ReadCatalog(t.Context())
	if err != nil || c.Entries[0].Project.ID != "1" || c.Entries[1].Project.ID != "2" {
		t.Fatalf("sort: %+v %v", c, err)
	}
}
func TestRestoreSeparatePreferencesAndNoFallback(t *testing.T) {
	s, r, d := fixture()
	r.snapshot = Snapshot{Projects: []domain.Project{{ID: "a", CanonicalPath: "/a"}, {ID: "b", CanonicalPath: "/b"}}, LastOpened: map[domain.ApplicationID]domain.ID{domain.TUI: "a", domain.GUI: "b"}}
	for _, app := range domain.Applications() {
		got, err := s.RestoreLastProject(t.Context(), app)
		if err != nil || got.Kind != RestoreReady || got.Project.ID != r.snapshot.LastOpened[app] {
			t.Fatalf("%+v %v", got, err)
		}
	}
	d.result.Kind = domain.Missing
	got, err := s.RestoreLastProject(t.Context(), domain.TUI)
	if err != nil || got.Kind != RestoreUnavailable || got.Project.ID != "a" || len(r.snapshot.Projects) != 2 {
		t.Fatalf("missing: %+v %v", got, err)
	}
	r.snapshot.LastOpened[domain.TUI] = "dangling"
	got, err = s.RestoreLastProject(t.Context(), domain.TUI)
	if got.Kind != RestoreDangling || !errors.Is(err, ErrDanglingPreference) {
		t.Fatalf("dangling: %+v %v", got, err)
	}
	delete(r.snapshot.LastOpened, domain.TUI)
	got, err = s.RestoreLastProject(t.Context(), domain.TUI)
	if err != nil || got.Kind != RestoreNone {
		t.Fatalf("none: %+v %v", got, err)
	}
}
func TestRecordRevalidatesAndRejectsInvalidApp(t *testing.T) {
	s, r, d := fixture()
	r.snapshot.Projects = []domain.Project{{ID: "p", CanonicalPath: "/project"}}
	d.result.Kind = domain.Missing
	got, err := s.RecordOpenedProject(t.Context(), domain.TUI, "p")
	if err != nil || got.Persisted || r.writeCalls != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	_, err = s.RecordOpenedProject(t.Context(), "other", "p")
	if !errors.Is(err, ErrInvalidApplication) {
		t.Fatal(err)
	}
	_, err = s.RecordOpenedProject(t.Context(), domain.TUI, "unknown")
	if !errors.Is(err, ErrUnknownProject) {
		t.Fatal(err)
	}
}
func TestPartialSuccessPreferenceFailure(t *testing.T) {
	s, r, _ := fixture()
	cause := errors.New("write refused")
	r.errorWrite = cause
	got, err := s.RegisterAndRecordOpened(t.Context(), domain.TUI, "input", "/")
	if !errors.Is(err, cause) || !got.Registration.Persisted || got.Opened.Persisted || len(r.snapshot.Projects) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	r.errorWrite = nil
	retry, err := s.RegisterAndRecordOpened(t.Context(), domain.TUI, "input", "/")
	if err != nil || retry.Registration.Project.ID != got.Registration.Project.ID || !retry.Opened.Persisted || r.insertCalls != 1 {
		t.Fatalf("retry %+v %v", retry, err)
	}
}
func TestCancellationStopsNextEffect(t *testing.T) {
	s, r, d := fixture()
	ctx, cancel := context.WithCancel(t.Context())
	d.afterRead = cancel
	_, err := s.RegisterLocalProject(ctx, "input", "/")
	if !errors.Is(err, context.Canceled) || r.insertCalls != 0 {
		t.Fatal(err)
	}
	s, r, d = fixture()
	r.snapshot.Projects = []domain.Project{{ID: "p", CanonicalPath: "/project"}}
	ctx, cancel = context.WithCancel(t.Context())
	d.afterRead = cancel
	_, err = s.RecordOpenedProject(ctx, domain.TUI, "p")
	if !errors.Is(err, context.Canceled) || r.writeCalls != 0 {
		t.Fatal(err)
	}
}
func TestCatalogAndCompletedWriteErrors(t *testing.T) {
	for _, cause := range []error{ErrCorruptCatalog, ErrFutureCatalog, context.Canceled} {
		s, r, _ := fixture()
		r.readErr = cause
		_, err := s.ReadCatalog(t.Context())
		if !errors.Is(err, cause) {
			t.Fatal(err)
		}
	}
	s, r, _ := fixture()
	r.errorInsert = context.Canceled
	r.persistedOnError = true
	got, err := s.RegisterAndRecordOpened(t.Context(), domain.TUI, "input", "/")
	if !errors.Is(err, context.Canceled) || !got.Registration.Persisted || r.writeCalls != 0 || len(r.snapshot.Projects) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDistinctDirectoriesWithSameName(t *testing.T) {
	s, r, d := fixture()
	first, err := s.RegisterLocalProject(t.Context(), "/one/project", "/")
	if err != nil {
		t.Fatal(err)
	}
	d.result.CanonicalPath = "/other/project"
	second, err := s.RegisterLocalProject(t.Context(), "/other/project", "/")
	if err != nil || second.Project.ID == first.Project.ID || second.Project.Name != first.Project.Name || len(r.snapshot.Projects) != 2 {
		t.Fatalf("first %+v second %+v err %v", first, second, err)
	}
}
func TestUnavailableWithoutTechnicalFailure(t *testing.T) {
	s, r, d := fixture()
	d.result.Kind = domain.Missing
	got, err := s.RegisterLocalProject(t.Context(), "input", "/")
	if err != nil || got.Kind != domain.RegistrationRejected || r.insertCalls != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCatalogAvailabilityErrorsRetainEveryEntry(t *testing.T) {
	s, r, d := fixture()
	cause := errors.New("directory unavailable")
	r.snapshot.Projects = []domain.Project{{ID: "a", Name: "a", CanonicalPath: "/a"}, {ID: "b", Name: "b", CanonicalPath: "/b"}}
	d.result.Kind = domain.Unreadable
	d.result.Cause = cause
	d.err = cause
	got, err := s.ReadCatalog(t.Context())
	if !errors.Is(err, cause) || len(got.Entries) != 2 || d.calls != 2 || !errors.Is(got.Entries[1].Directory.Cause, cause) {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCatalogStopsOnCancellationOrInvalidObservation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("invalid adapter response")} {
		s, r, d := fixture()
		r.snapshot.Projects = []domain.Project{{ID: "a", Name: "a", CanonicalPath: "/a"}, {ID: "b", Name: "b", CanonicalPath: "/b"}}
		d.err = cause
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			d.result.Kind = domain.Unreadable
		}
		got, err := s.ReadCatalog(t.Context())
		if !errors.Is(err, cause) || len(got.Entries) != 0 || d.calls != 1 {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
