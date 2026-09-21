package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestLoadingSkipsFastWork(t *testing.T) {
	want := errors.New("work failed")
	calls := 0
	err := runLoadingWithRunner("Search", "Loading", func(context.Context) error { calls++; return want }, func(tea.Model) (tea.Model, error) {
		t.Fatal("fast work must not flash a loading screen")
		return nil, nil
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)
}

func TestLoadingAnimationUsesOneSmoothCadence(t *testing.T) {
	m := newLoadingModel("Search", "Loading", func() {}, make(chan error))
	require.GreaterOrEqual(t, len(m.spin.Spinner.Frames), 8)
	require.LessOrEqual(t, m.spin.Spinner.FPS, 100*time.Millisecond)

	next := m.now.Add(m.spin.Spinner.FPS)
	m.Update(spinner.TickMsg{Time: next, ID: m.spin.ID()})
	require.Equal(t, next, m.now)
}

func TestLoadingRendererExitAfterResultConsumed(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan error, 1)
	rendererErr := errors.New("renderer stopped")
	go func() {
		finished <- runLoadingWithRunner("Search", "Loading", func(context.Context) error {
			<-release
			return nil
		}, func(model tea.Model) (tea.Model, error) {
			close(release)
			loading := model.(*loadingModel)
			loading.Update(loading.await()()) // Result consumed before shutdown.
			return model, rendererErr
		})
	}()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, rendererErr)
	case <-time.After(3 * time.Second):
		t.Fatal("waiting for an already consumed result")
	}
}

func TestLoadingRendererInterruptCancelsWork(t *testing.T) {
	err := runLoadingWithRunner("Search", "Loading", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, func(model tea.Model) (tea.Model, error) { return model, tea.ErrInterrupted })
	require.ErrorIs(t, err, tea.ErrInterrupted)
	require.True(t, IsCancelled(err))
}

func TestPromptLogOverlayDoesNotSubmitOrEdit(t *testing.T) {
	model := newPromptModel(PromptOptions{Title: "Change Anime", MinLength: 2})
	model.input.SetValue("Naruto")
	model.shell.Logs = true
	model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	require.Equal(t, "Naruto", model.input.Value())
	_, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	require.False(t, model.submit)
	require.False(t, model.shell.Logs)
}
