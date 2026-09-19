package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// activePrograms counts shell screens currently on the terminal. RunLoading
// degrades to running the work inline when one is already up: two Bubble Tea
// programs on one terminal corrupt each other.
var activePrograms atomic.Int32

// busy marks a shell screen as running for the duration of run.
func busy(run func() error) error {
	activePrograms.Add(1)
	defer activePrograms.Add(-1)
	return run()
}

// loadingDoneMsg carries the work's result back into the program.
type loadingDoneMsg struct{ err error }

type tickMsg time.Time

type loadingModel struct {
	shell   Shell
	theme   Theme
	spin    spinner.Model
	title   string
	started time.Time
	now     time.Time
	done    bool
	err     error
	cancel  context.CancelFunc
	result  <-chan error
	stopped bool
}

func newLoadingModel(breadcrumb, title string, cancel context.CancelFunc, result <-chan error) *loadingModel {
	theme := NewTheme(true)
	sp := spinner.New(spinner.WithSpinner(spinner.Points), spinner.WithStyle(theme.Primary))
	now := time.Now()
	return &loadingModel{
		shell:   NewShell(&theme, singleLine(breadcrumb)),
		theme:   theme,
		spin:    sp,
		title:   singleLine(title),
		started: now,
		now:     now,
		cancel:  cancel,
		result:  result,
	}
}

func (m *loadingModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.await(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *loadingModel) await() tea.Cmd {
	result := m.result
	return func() tea.Msg { return loadingDoneMsg{err: <-result} }
}

func (m *loadingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.shell.Resize(msg.Width, msg.Height)
		return m, nil
	case loadingDoneMsg:
		m.done, m.err = true, msg.err
		return m, tea.Quit
	case tickMsg:
		m.now = time.Time(msg)
		return m, tick()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			if m.shell.Logs && msg.String() != "ctrl+c" {
				m.shell.Logs = false
				return m, nil
			}
			// Cancel the work; the done message still arrives and quits.
			m.stopped = true
			m.cancel()
			return m, nil
		case LogsToggleKey:
			m.shell.ToggleLogs()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	return m, cmd
}

func (m *loadingModel) View() tea.View {
	elapsed := m.now.Sub(m.started).Round(100 * time.Millisecond)
	status := m.theme.Muted.Render(fmt.Sprintf("%.1fs", elapsed.Seconds()))
	if m.stopped {
		status = m.theme.Warn.Render("cancelling…")
	}
	body := "\n  " + m.spin.View() + "  " + m.theme.Value.Render(m.title) + "   " + status
	view := tea.NewView(m.shell.Render(body, "esc cancel"))
	view.AltScreen = true
	view.WindowTitle = "GonimeId - " + m.title
	return view
}

// RunLoading shows an animated, cancellable loading screen in the shell chrome
// while work runs. It returns the work's error, or ErrPickCancelled when the
// user pressed Esc/ctrl+c (the work sees its context cancelled).
//
// Without a terminal, or while another shell screen is already up, the work
// simply runs inline: the screen is decoration, the work is the point.
func RunLoading(breadcrumb, title string, work func(ctx context.Context) error) error {
	if activePrograms.Load() > 0 || !term.IsTerminal(int(os.Stdin.Fd())) {
		return work(context.Background())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- work(ctx) }()

	model := newLoadingModel(breadcrumb, title, cancel, result)
	runErr := RunClean(func() error {
		return busy(func() error {
			_, err := NewProgram(model).Run()
			return err
		})
	})
	if runErr != nil {
		// The screen failed, not the work: wait for the work and report it.
		return <-result
	}
	if model.stopped {
		return ErrPickCancelled
	}
	return model.err
}

// IsCancelled reports whether err came from the user backing out of a screen
// (Esc/ctrl+c), as opposed to the work itself failing.
func IsCancelled(err error) bool {
	return errors.Is(err, ErrPickCancelled) || errors.Is(err, ErrPickBack) || errors.Is(err, context.Canceled)
}
