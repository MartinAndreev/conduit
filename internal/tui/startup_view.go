package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *Model) View() tea.View {
	width := max(1, m.width-4)
	welcome := m.outcome != nil && (m.snapshot.Status == startup.Ready || m.snapshot.Status == startup.ReadyWithWarnings)
	if welcome {
		width = min(width, 86)
	}
	heading := m.text("startup.heading")
	body := m.startupView()
	if m.outcome != nil && (m.snapshot.Status == startup.Ready || m.snapshot.Status == startup.ReadyWithWarnings) {
		heading = m.text("welcome.heading")
		body = m.welcomeView()
	}
	header := theme.title.Render(ansi.Wrap(heading, width, ""))
	content := ansi.Wrap(body, width, "")
	footer := ansi.Wrap(m.actionView()+"\n"+theme.muted.Render(m.text("keyboard.hints")), width, "")
	lines := strings.Split(content, "\n")
	available := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-3)
	offset := min(m.offset, max(0, len(lines)-available))
	end := min(len(lines), offset+available)
	content = strings.Join(lines[offset:end], "\n")
	panelHeight := available
	if welcome {
		panelHeight = min(available, len(lines))
	}
	content = lipgloss.NewStyle().Width(width).Height(panelHeight).Border(lipgloss.NormalBorder()).Padding(0, 1).Render(content)
	if welcome {
		content = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
		content = lipgloss.PlaceVertical(available+2, lipgloss.Center, content)
	}
	output := lipgloss.JoinVertical(lipgloss.Left, header, "", content, footer)
	// Hard crop also handles terminals smaller than the usual minimum viewport.
	rendered := strings.Split(output, "\n")
	for i, line := range rendered {
		rendered[i] = ansi.Truncate(line, m.width, "")
	}
	if len(rendered) > m.height {
		rendered = rendered[:m.height]
	}
	view := tea.NewView(strings.Join(rendered, "\n"))
	view.AltScreen = true
	return view
}

func (m *Model) startupView() string {
	var out strings.Builder
	title := m.text("title.startup")
	switch m.snapshot.Status {
	case startup.Blocked:
		title = m.text("title.blocked")
	case startup.Cancelled:
		title = m.text("title.cancelled")
	}
	if m.cancelling && m.outcome == nil {
		title = m.text("title.cancelling")
	}
	fmt.Fprintln(&out, theme.title.Render(title))
	fmt.Fprintln(&out, m.text("startup.intro"))
	if m.snapshot.Status == startup.Blocked {
		for _, d := range m.snapshot.Diagnostics {
			fmt.Fprintln(&out, theme.warning.Render(diagnosticText(m.locale, d)))
		}
	}
	finished := 0
	for _, s := range m.snapshot.Steps {
		if s.Status == startup.Completed || s.Status == startup.Skipped {
			finished++
		}
	}
	fmt.Fprintf(&out, "\n"+m.text("startup.steps")+"\n\n", finished, 4)
	labels := map[startup.StepID]string{startup.CheckConfiguration: m.text("step.check_configuration"), startup.DiscoverHarnesses: m.text("step.discover_harnesses"), startup.CreateSettings: m.text("step.create_settings"), startup.CheckReadiness: m.text("step.check_readiness")}
	for i, s := range m.snapshot.Steps {
		symbol, status := "·", m.text("status.pending")
		switch s.Status {
		case startup.Working:
			symbol = m.spinner.View()
			status = m.text("status.running")
		case startup.Completed:
			symbol = "✓"
			status = m.text("status.completed")
		case startup.Skipped:
			symbol = "−"
			status = m.text("status.skipped")
		case startup.Failed:
			symbol = "!"
			status = m.text("status.failed")
		case startup.StepCancelled:
			symbol = "×"
			status = m.text("status.cancelled")
		}
		line := fmt.Sprintf("%s %d  %s\n     %s", symbol, i+1, labels[s.ID], status)
		if s.Status == startup.Working {
			line = theme.active.Render(line)
		}
		fmt.Fprintln(&out, line)
	}
	for _, observation := range m.snapshot.Observations {
		fmt.Fprintf(&out, "\n%s: %s", observation.Harness, m.text("observation."+string(observation.Discovery.Kind)))
		if observation.Discovery.Version != "" {
			fmt.Fprintf(&out, " · %s", safeText(observation.Discovery.Version))
		}
	}
	fmt.Fprintln(&out, "\n\n"+m.text("startup.harness_notice"))
	out.WriteString(m.detailsView())
	return out.String()
}

func (m *Model) detailsView() string {
	var out strings.Builder
	if m.snapshot.ConfigPath != "" {
		fmt.Fprintf(&out, "\n"+m.text("label.configuration")+"\n", safeText(m.snapshot.ConfigPath))
	}
	for _, d := range m.snapshot.Diagnostics {
		if m.snapshot.Status == startup.Blocked {
			continue
		}
		fmt.Fprintln(&out, theme.warning.Render(diagnosticText(m.locale, d)))
	}
	if m.outcome != nil && m.outcome.Published && m.snapshot.Status != startup.Ready && m.snapshot.Status != startup.ReadyWithWarnings {
		fmt.Fprintln(&out, m.text("startup.published"))
	}
	if m.hasPendingCleanup() || (m.exiting && m.outcome == nil) {
		key := "cleanup.pending"
		if m.outcome == nil {
			key = "cleanup.active"
		}
		fmt.Fprintln(&out, theme.warning.Render(m.text(key)))
		if m.cleanupWaiting {
			fmt.Fprintln(&out, m.text("cleanup.waiting"))
		} else if m.cleanupExpired {
			fmt.Fprintln(&out, m.text("cleanup.expired"))
		}
	}
	return out.String()
}

func (m *Model) actionView() string {
	labels := map[string]string{"cancel": m.text("action.cancel"), "exit": m.text("action.exit"), "retry": m.text("action.retry"), "wait": m.text("action.wait"), "exit_now": m.text("action.exit_now")}
	actions := m.actions()
	focus := min(m.focus, len(actions)-1)
	var rendered []string
	for i, a := range actions {
		label := "[ " + labels[a] + " ]"
		if i == focus {
			label = theme.active.Render(label)
		}
		rendered = append(rendered, label)
	}
	return strings.Join(rendered, "  ")
}
