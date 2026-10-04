package tui

func (m *Model) welcomeView() string {
	return theme.title.Render(m.text("welcome.title")) + "\n\n" + m.text("welcome.intro") + "\n\n" +
		m.text("welcome.step1") + "\n" + m.text("welcome.step2") + "\n" + m.text("welcome.step3") + "\n\n" +
		theme.muted.Render(m.text("welcome.unavailable")) + "\n\n" + m.text("welcome.tagline") + "\n" + m.text("welcome.unknown") + "\n" + m.detailsView()
}
