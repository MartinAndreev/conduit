package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeInitializer struct{ calls atomic.Int32 }
type delayedInitializer struct {
	entered chan struct{}
	release chan struct{}
}

func (f delayedInitializer) Initialize(ctx context.Context, p *startup.Progress) startup.Outcome {
	close(f.entered)
	<-f.release
	return startup.Outcome{Snapshot: startup.Snapshot{Status: startup.Cancelled}, Err: ctx.Err()}
}

func TestExitDeadlineWhileInitializerIsStillReturning(t *testing.T) {
	service := delayedInitializer{entered: make(chan struct{}), release: make(chan struct{})}
	m := New(service)
	m.begin()
	result := make(chan tea.Msg, 1)
	go func() { result <- m.active.execute(service)() }()
	<-service.entered
	command := m.perform("exit")
	if command == nil {
		t.Fatal("no bounded active-operation exit wait")
	}
	started := time.Now()
	m.Update(command())
	if time.Since(started) > time.Second || m.outcome != nil || !m.cleanupExpired {
		t.Fatal("active wait not bounded")
	}
	if m.perform("retry") != nil || m.perform("exit_now") == nil {
		t.Fatal("wrong pending operation controls")
	}
	if m.active == nil {
		t.Fatal("active owner lost")
	}
	close(service.release)
	m.Update(<-result)
	m.active.progress.Close()
}

func (f *fakeInitializer) Initialize(ctx context.Context, p *startup.Progress) startup.Outcome {
	f.calls.Add(1)
	<-ctx.Done()
	return startup.Outcome{Snapshot: startup.Snapshot{OperationID: "fake", Status: startup.Cancelled}, Err: ctx.Err()}
}

type fakeObserver struct {
	complete atomic.Bool
	done     chan struct{}
}

func (f *fakeObserver) Latest() startup.CleanupObservation {
	return startup.CleanupObservation{Complete: f.complete.Load()}
}
func (f *fakeObserver) Done() <-chan struct{} { return f.done }
func (f *fakeObserver) Await(ctx context.Context) (startup.CleanupObservation, error) {
	if f.complete.Load() {
		return f.Latest(), nil
	}
	select {
	case <-ctx.Done():
		return f.Latest(), ctx.Err()
	case <-f.done:
		return f.Latest(), nil
	}
}

func TestCancelOutcomeAndRetryGeneration(t *testing.T) {
	service := &fakeInitializer{}
	m := New(service)
	m.begin() // Commands aren't executed by this pure event-loop test.
	old := m.generation
	if m.perform("retry") != nil || m.generation != old {
		t.Fatal("retry started while active")
	}
	m.perform("cancel")
	if m.active.ctx.Err() != context.Canceled || m.outcome != nil {
		t.Fatal("cancel fabricated terminal result")
	}
	m.Update(outcomeMsg{old, startup.Outcome{Snapshot: startup.Snapshot{OperationID: "old", Revision: 4, Status: startup.Cancelled}}})
	if m.perform("retry") == nil || m.generation == old {
		t.Fatal("retry not started")
	}
	fresh := m.generation
	m.perform("retry")
	if m.generation != fresh {
		t.Fatal("double retry")
	}
	m.Update(progressMsg{old, startup.Snapshot{OperationID: "old", Status: startup.Ready, Revision: 100}})
	m.Update(outcomeMsg{old, startup.Outcome{Snapshot: startup.Snapshot{Status: startup.Ready}}})
	if m.outcome != nil || m.snapshot.Status != startup.Running {
		t.Fatal("old operation replaced new state")
	}
	m.active.cancel()
	m.active.progress.Close()
}

func TestOutcomeWinsOverProgressAndRevision(t *testing.T) {
	m := New(&fakeInitializer{})
	m.begin()
	g := m.generation
	m.Update(progressMsg{g, startup.Snapshot{OperationID: "one", Revision: 4, Status: startup.Running}})
	m.Update(progressMsg{g, startup.Snapshot{OperationID: "two", Revision: 99, Status: startup.Ready}})
	if m.snapshot.OperationID != "one" {
		t.Fatal("foreign operation accepted")
	}
	m.Update(outcomeMsg{g, startup.Outcome{Snapshot: startup.Snapshot{OperationID: "one", Revision: 5, Status: startup.Blocked}}})
	m.Update(progressMsg{g, startup.Snapshot{OperationID: "one", Revision: 100, Status: startup.Ready}})
	if m.snapshot.Status != startup.Blocked {
		t.Fatal("progress replaced outcome")
	}
	m.active.cancel()
}

func TestPendingCleanupWaitAndExplicitExit(t *testing.T) {
	m := New(&fakeInitializer{})
	m.begin()
	observer := &fakeObserver{done: make(chan struct{})}
	commandModel, command := m.Update(outcomeMsg{m.generation, startup.Outcome{Snapshot: startup.Snapshot{Status: startup.Blocked}, PendingCleanup: observer}})
	if commandModel != m || command == nil {
		t.Fatal("cleanup not awaited")
	}
	if m.perform("retry") != nil {
		t.Fatal("retry allowed during cleanup")
	}
	if m.perform("exit_now") != nil {
		t.Fatal("force exit before bounded wait")
	}
	started := time.Now()
	message := command()
	if time.Since(started) > time.Second {
		t.Fatal("wait was not bounded")
	}
	m.Update(message)
	if !m.cleanupExpired || m.outcome.PendingCleanup != observer {
		t.Fatal("cleanup ownership lost")
	}
	if command := m.perform("exit_now"); command == nil {
		t.Fatal("explicit exit unavailable")
	}
	observer.complete.Store(true)
	close(observer.done)
	if m.perform("retry") == nil {
		t.Fatal("retry unavailable after cleanup")
	}
	m.active.cancel()
	m.active.progress.Close()
}

func TestFixtureRenderSizesAndSafeFallback(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {36, 12}, {1, 1}} {
		m := New(&fakeInitializer{})
		m.snapshot = startup.Snapshot{Status: startup.Blocked, ConfigPath: "/tmp/" + strings.Repeat("дълъг-път/", 30), Diagnostics: []startup.Diagnostic{{Code: "unknown\x1b[31m"}}}
		m.outcome = &startup.Outcome{Snapshot: m.snapshot}
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		rendered := m.View().Content
		lines := strings.Split(rendered, "\n")
		if len(lines) > size[1] {
			t.Fatalf("%v: rows %d", size, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v: oversized line width %d: %q", size, ansi.StringWidth(line), line)
			}
		}
	}
	text := diagnosticText(i18n.Bulgarian, startup.Diagnostic{Code: "unknown\x1b[31m"})
	if strings.Contains(text, "\x1b") || !strings.Contains(text, "Неизвестна") {
		t.Fatal("unsafe fallback")
	}
	m := New(&fakeInitializer{})
	m.snapshot.Status = startup.Ready
	m.outcome = &startup.Outcome{Snapshot: m.snapshot}
	if !strings.Contains(m.welcomeView(), "Unavailable") || len(m.actions()) != 1 {
		t.Fatal("fake welcome actions")
	}
}

func TestLocaleSelection(t *testing.T) {
	for _, fixture := range []struct {
		locale   i18n.Locale
		expected string
	}{{"de", "Preparing Conduit"}, {i18n.English, "Preparing Conduit"}, {i18n.Bulgarian, "Подготвяме Conduit"}} {
		m := New(&fakeInitializer{}, fixture.locale)
		if !strings.Contains(m.startupView(), fixture.expected) {
			t.Fatalf("locale %q did not select %q", fixture.locale, fixture.expected)
		}
	}
}

func TestUnexpectedProgramExitCancelsOperation(t *testing.T) {
	service := &fakeInitializer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(service, tea.WithContext(ctx), tea.WithInput(nil), tea.WithoutRenderer()) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for service.calls.Load() == 0 {
		select {
		case <-deadline.C:
			t.Fatal("startup not executed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unexpected terminal exit did not terminate boundedly")
	}
	if service.calls.Load() != 1 {
		t.Fatal("more than one startup")
	}
}
