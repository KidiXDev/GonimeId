package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type countdownTick struct{}

type countdownModel struct {
	theme     Theme
	shell     Shell
	title     string
	remaining int
	advance   bool
}

func newCountdownModel(title string, seconds int) *countdownModel {
	theme := NewTheme(true)
	shell := NewShell(&theme, "Up next")
	shell.CompactFooter = "enter now · esc cancel"
	return &countdownModel{
		theme: theme, shell: shell,
		title: singleLine(title), remaining: max(seconds, 1),
	}
}

func countdownCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return countdownTick{}
	}
}

func (m *countdownModel) Init() tea.Cmd { return countdownCmd() }

func (m *countdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.shell.Resize(msg.Width, msg.Height)
		return m, nil
	case countdownTick:
		m.remaining--
		if m.remaining <= 0 {
			m.advance = true
			return m, tea.Quit
		}
		return m, countdownCmd()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter", "space":
			m.advance = true
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *countdownModel) View() tea.View {
	width, height := m.shell.ContentSize()
	message := lipgloss.JoinVertical(lipgloss.Center,
		m.theme.Muted.Render("Episode complete"),
		"",
		m.theme.Value.Render(m.title),
		"",
		m.theme.Primary.Render(fmt.Sprintf("Playing in %d…", m.remaining)),
	)
	panelWidth := min(max(width-8, 24), 64)
	panel := m.theme.Panel.Width(panelWidth).Align(lipgloss.Center).Render(message)
	body := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, panel)
	view := tea.NewView(m.shell.Render(body, "enter play now · esc cancel"))
	view.AltScreen = true
	view.WindowTitle = "GonimeId - Up Next"
	return view
}

// AutoplayCountdown waits before advancing and remains keyboard-cancellable.
func AutoplayCountdown(title string, seconds int) (bool, error) {
	final, err := runScreen(newCountdownModel(title, seconds))
	if err != nil {
		return false, fmt.Errorf("run autoplay countdown: %w", err)
	}
	model, ok := final.(*countdownModel)
	if !ok || model == nil {
		return false, fmt.Errorf("unexpected autoplay model %T", final)
	}
	return model.advance, nil
}
