package tui

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/configuration"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"github.com/charmbracelet/colorprofile"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureModel drives the real Bubble Tea renderer with safe immutable fixtures.
// The fixture never constructs production file or process adapters.
type captureModel struct {
	model  *Model
	resize bool
}

func (f captureModel) Init() tea.Cmd {
	if f.resize {
		return tea.Sequence(func() tea.Msg { return tea.WindowSizeMsg{Width: 120, Height: 40} }, tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg { return tea.WindowSizeMsg{Width: 60, Height: 20} }), tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() }))
	}
	return tea.Sequence(func() tea.Msg { return tea.WindowSizeMsg{Width: f.model.width, Height: f.model.height} }, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() }))
}
func (f captureModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	_, command := f.model.Update(message)
	return f, command
}
func (f captureModel) View() tea.View { return f.model.View() }

func TestCaptureFixtures(t *testing.T) {
	directory := os.Getenv("CONDUIT_TUI_CAPTURE_DIR")
	if directory == "" {
		t.Skip("opt-in real terminal renderer evidence")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name          string
		status        startup.Status
		width, height int
	}{
		{"startup-120x40", startup.Running, 120, 40}, {"startup-80x24", startup.Running, 80, 24},
		{"welcome-120x40", startup.ReadyWithWarnings, 120, 40}, {"welcome-80x24", startup.ReadyWithWarnings, 80, 24},
		{"startup-80x24-bg", startup.Running, 80, 24}, {"welcome-80x24-bg", startup.ReadyWithWarnings, 80, 24},
		{"blocked-80x24", startup.Blocked, 80, 24}, {"cancelled-80x24", startup.Cancelled, 80, 24}, {"resize-60x20", startup.Running, 60, 20},
	} {
		m := New(&fakeInitializer{})
		if strings.HasSuffix(fixture.name, "-bg") {
			m.locale = i18n.Bulgarian
		}
		m.width = fixture.width
		m.height = fixture.height
		m.snapshot = startup.Snapshot{OperationID: "synthetic-startup", Revision: 4, Status: fixture.status, ConfigPath: "/synthetic/home/.conduit/config.yaml", Steps: []startup.StepState{{ID: startup.CheckConfiguration, Status: startup.Completed}, {ID: startup.DiscoverHarnesses, Status: startup.Completed}, {ID: startup.CreateSettings, Status: startup.Working}, {ID: startup.CheckReadiness, Status: startup.Pending}}, Observations: []startup.HarnessObservation{{Harness: configuration.Codex, Discovery: configuration.Observation{Kind: configuration.Found, Version: "1.0-fixture"}}, {Harness: configuration.OpenCode, Discovery: configuration.Observation{Kind: configuration.Missing}}}}
		if fixture.status != startup.Running {
			m.outcome = &startup.Outcome{Snapshot: m.snapshot}
		}
		if fixture.status == startup.Blocked {
			m.snapshot.Diagnostics = []startup.Diagnostic{{Code: "configuration_invalid"}}
			m.snapshot.Steps[0].Status = startup.Failed
			for i := 1; i < len(m.snapshot.Steps); i++ {
				m.snapshot.Steps[i].Status = startup.Pending
			}
			m.snapshot.Observations = nil
		}
		if fixture.status == startup.Cancelled {
			m.snapshot.Steps[2].Status = startup.StepCancelled
		}
		if fixture.status == startup.ReadyWithWarnings {
			m.snapshot.Diagnostics = []startup.Diagnostic{{Code: "harness_missing", Harness: configuration.OpenCode}}
		}
		var output bytes.Buffer
		_, err := tea.NewProgram(captureModel{model: m, resize: strings.HasPrefix(fixture.name, "resize-")}, tea.WithInput(nil), tea.WithOutput(&output), tea.WithColorProfile(colorprofile.TrueColor), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			// No PTY is attached; ignore its empty size query, retaining only
			// the explicit fixture WindowSizeMsg events consumed by the renderer.
			if size, ok := msg.(tea.WindowSizeMsg); ok && (size.Width <= 0 || size.Height <= 0) {
				return nil
			}
			return msg
		})).Run()
		if err != nil {
			t.Fatal(err)
		}
		// Retain the renderer's frame before alternate-screen restoration on exit.
		stream := output.Bytes()
		if end := bytes.LastIndex(stream, []byte("\x1b[?1049l")); end >= 0 {
			stream = stream[:end]
		}
		if len(stream) < 100 {
			t.Fatalf("%s: renderer did not produce a frame", fixture.name)
		}
		if err := os.WriteFile(filepath.Join(directory, fixture.name+".ansi"), stream, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
