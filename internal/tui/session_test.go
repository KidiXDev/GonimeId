package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestScreenSessionKeepsAlternateScreenAcrossNavigation(t *testing.T) {
	var output bytes.Buffer
	p := tea.NewProgram(&screenSessionModel{}, BubbleTeaProgramOptions(
		tea.WithInput(nil), tea.WithOutput(&output), tea.WithoutSignals(), tea.WithWindowSize(100, 30),
	)...)
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	t.Cleanup(p.Kill)
	run := func(model tea.Model, key string) tea.Model {
		t.Helper()
		result := make(chan tea.Model, 1)
		p.Send(screenRequest{model, result})
		if key != "" {
			p.Send(tea.KeyPressMsg{Code: map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape}[key]})
		}
		select {
		case final := <-result:
			return final
		case err := <-done:
			t.Fatalf("session exited during navigation: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("screen did not complete")
		}
		return nil
	}
	for i := 0; i < 3; i++ {
		menu := newPickerModel([]PickItem{{Label: "Open"}}, PickOptions{})
		require.Same(t, menu, run(menu, "enter"))
		require.Equal(t, 0, menu.selectedIndex)
		prompt := newPromptModel(PromptOptions{Title: "Search"})
		require.Same(t, prompt, run(prompt, "esc"))
		require.ErrorIs(t, prompt.err, ErrPickBack)
		require.Equal(t, 100, prompt.shell.Width)
		require.Equal(t, 30, prompt.shell.Height)
	}
	result := make(chan error, 1)
	result <- nil
	loading := newLoadingModel("Search", "Loading", func() {}, result)
	require.Same(t, loading, run(loading, ""))
	require.True(t, loading.done)
	p.Quit()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("session did not exit")
	}
	require.Equal(t, 1, strings.Count(output.String(), "\x1b[?1049h"), "navigation must enter the alternate screen only once")
	require.Equal(t, 1, strings.Count(output.String(), "\x1b[?1049l"), "only session shutdown may leave the alternate screen")
}

func TestScreenSessionFreezesFrameAndDiscardsPreviousCommands(t *testing.T) {
	session := &screenSessionModel{}
	first := newPromptModel(PromptOptions{Title: "First"})
	result := make(chan tea.Model, 1)
	session.Update(screenRequest{first, result})
	oldGeneration := session.generation
	before := session.View().Content
	_, cmd := session.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	session.Update(cmd())
	require.Same(t, first, <-result)
	require.Equal(t, before, session.View().Content)
	session.Update(screenMessage{oldGeneration, tea.KeyPressMsg{Code: 'x', Text: "x"}})
	require.Empty(t, first.input.Value(), "completed models must no longer be mutated")

	second := newPromptModel(PromptOptions{Title: "Second"})
	session.Update(screenRequest{second, make(chan tea.Model, 1)})
	session.Update(screenMessage{oldGeneration, tea.QuitMsg{}})
	session.Update(screenMessage{oldGeneration, tea.KeyPressMsg{Code: 'x', Text: "x"}})
	require.Same(t, second, session.child)
	require.Empty(t, second.input.Value())
}

func TestCloseScreensReleasesRendererAndPendingScreen(t *testing.T) {
	s := &screenSession{done: make(chan struct{})}
	s.program = tea.NewProgram(&screenSessionModel{}, BubbleTeaProgramOptions(tea.WithInput(nil), tea.WithOutput(&bytes.Buffer{}), tea.WithoutSignals())...)
	screens.Lock()
	screens.session = s
	screens.Unlock()
	go func() { _, s.err = s.program.Run(); close(s.done) }()
	s.program.Send(screenRequest{newPromptModel(PromptOptions{Title: "Pending"}), make(chan tea.Model, 1)})
	require.True(t, ScreensActive())
	ResetTerminal() // Must neither show the hardware cursor nor drain input.
	closed := make(chan struct{})
	go func() { CloseScreens(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		s.program.Kill()
		t.Fatal("close blocked with an active screen")
	}
	require.NoError(t, s.err)
	require.False(t, ScreensActive())
	screens.Lock()
	require.Nil(t, screens.session)
	screens.Unlock()
	CloseScreens() // Idempotent exit cleanup.
}

func TestScreenSessionAllowsInterruptBetweenScreens(t *testing.T) {
	session := &screenSessionModel{}
	_, cmd := session.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	require.IsType(t, tea.InterruptMsg{}, cmd())
}
