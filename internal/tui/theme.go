package tui

import "charm.land/lipgloss/v2"

var theme = struct{ title, muted, active, warning lipgloss.Style }{
	title:   lipgloss.NewStyle().Bold(true),
	muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	active:  lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true),
	warning: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
}
