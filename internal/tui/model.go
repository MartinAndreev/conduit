// Package tui adapts application startup to the terminal. Views perform no I/O.
package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"time"
)

type Model struct {
	locale                                              i18n.Locale
	service                                             Initializer
	active                                              *operation
	generation                                          uint64
	snapshot                                            startup.Snapshot
	outcome                                             *startup.Outcome
	spinner                                             spinner.Model
	width, height                                       int
	offset                                              int
	focus                                               int
	cancelling, exiting, cleanupExpired, cleanupWaiting bool
	exitDeadline                                        time.Time
}

func New(service Initializer, locales ...i18n.Locale) *Model {
	locale := i18n.English
	if len(locales) > 0 {
		locale = i18n.Normalize(string(locales[0]))
	}
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	return &Model{service: service, locale: locale, spinner: s, width: 80, height: 24}
}
func (m *Model) Init() tea.Cmd { return tea.Batch(m.begin(), m.spinner.Tick) }

func (m *Model) begin() tea.Cmd {
	m.generation++
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &operation{generation: m.generation, ctx: ctx, cancel: cancel, progress: startup.NewProgress(), done: make(chan struct{})}
	m.outcome = nil
	m.snapshot = startup.Snapshot{Status: startup.Running}
	m.cancelling = false
	m.exiting = false
	m.cleanupExpired = false
	m.cleanupWaiting = false
	m.exitDeadline = time.Time{}
	m.offset = 0
	m.focus = 0
	return tea.Batch(m.active.execute(m.service), m.active.awaitProgress())
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		return m, nil
	case progressMsg:
		if msg.generation != m.generation || m.outcome != nil {
			return m, nil
		}
		if m.snapshot.OperationID == "" || (m.snapshot.OperationID == msg.snapshot.OperationID && msg.snapshot.Revision > m.snapshot.Revision) {
			m.snapshot = msg.snapshot
		}
		return m, m.active.awaitProgress()
	case outcomeMsg:
		if msg.generation != m.generation || m.outcome != nil {
			return m, nil
		}
		m.active.progress.Close()
		m.outcome = &msg.outcome
		m.snapshot = msg.outcome.Snapshot
		if m.hasPendingCleanup() {
			m.cleanupWaiting = true
			return m, m.waitCleanup()
		}
		if m.exiting {
			return m, tea.Quit
		}
		return m, nil
	case cleanupMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.cleanupWaiting = false
		m.cleanupExpired = msg.timedOut
		if msg.observation.Complete && m.exiting {
			return m, tea.Quit
		}
		return m, nil
	case exitDeadlineMsg:
		if msg.generation != m.generation || m.outcome != nil {
			return m, nil
		}
		m.cleanupWaiting = false
		m.cleanupExpired = msg.expired
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	case spinner.TickMsg:
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(msg)
		return m, command
	}
	return m, nil
}

func (m *Model) hasPendingCleanup() bool {
	return m.outcome != nil && m.outcome.PendingCleanup != nil && !m.outcome.PendingCleanup.Latest().Complete
}

func (m *Model) key(key string) tea.Cmd {
	switch key {
	case "down", "j":
		m.offset++
		return nil
	case "up", "k":
		m.offset = max(0, m.offset-1)
		return nil
	case "tab", "shift+tab":
		m.focus = (m.focus + 1) % len(m.actions())
		return nil
	case "enter":
		return m.perform(m.actions()[min(m.focus, len(m.actions())-1)])
	case "r":
		return m.perform("retry")
	case "esc":
		return m.perform("cancel")
	case "q", "ctrl+c":
		return m.perform("exit")
	}
	return nil
}

func (m *Model) actions() []string {
	if m.outcome == nil {
		if m.exiting {
			if m.cleanupExpired {
				return []string{"wait", "exit_now"}
			}
			return []string{"wait"}
		}
		return []string{"cancel", "exit"}
	}
	if m.hasPendingCleanup() {
		if m.cleanupExpired {
			return []string{"wait", "exit_now"}
		}
		return []string{"wait"}
	}
	if m.snapshot.Status == startup.Blocked || m.snapshot.Status == startup.Cancelled {
		return []string{"retry", "exit"}
	}
	return []string{"exit"}
}

func (m *Model) perform(action string) tea.Cmd {
	switch action {
	case "cancel":
		if m.outcome == nil {
			m.cancelling = true
			m.active.cancel()
		}
	case "exit":
		m.exiting = true
		if m.exitDeadline.IsZero() {
			m.exitDeadline = time.Now().Add(cleanupWait)
		}
		if m.outcome == nil {
			m.cancelling = true
			m.active.cancel()
			if !m.cleanupWaiting {
				m.cleanupWaiting = true
				return m.active.awaitExit()
			}
			return nil
		}
		if m.hasPendingCleanup() {
			if !m.cleanupWaiting {
				m.cleanupWaiting = true
				return m.waitCleanup()
			}
			return nil
		}
		return tea.Quit
	case "retry":
		if m.outcome != nil && !m.hasPendingCleanup() && (m.snapshot.Status == startup.Blocked || m.snapshot.Status == startup.Cancelled) {
			return m.begin()
		}
	case "wait":
		if m.outcome == nil && m.exiting && !m.cleanupWaiting {
			m.cleanupWaiting = true
			m.cleanupExpired = false
			m.exitDeadline = time.Now().Add(cleanupWait)
			return m.active.awaitExit()
		}
		if m.hasPendingCleanup() && !m.cleanupWaiting {
			m.cleanupWaiting = true
			m.cleanupExpired = false
			m.exitDeadline = time.Time{}
			return m.waitCleanup()
		}
	case "exit_now":
		if m.cleanupExpired && (m.hasPendingCleanup() || (m.outcome == nil && m.exiting)) {
			return tea.Quit
		}
	}
	return nil
}

func (m *Model) waitCleanup() tea.Cmd {
	wait := cleanupWait
	if !m.exitDeadline.IsZero() {
		wait = max(0, time.Until(m.exitDeadline))
	}
	return awaitCleanup(m.generation, m.outcome.PendingCleanup, wait)
}

func (m *Model) text(key string) string { return i18n.T(m.locale, key) }
