package tui

import (
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// A screen session owns one renderer across synchronous menu calls. Completing
// a child returns its result without leaving the alternate screen; the last
// frame remains visible until the next child is ready.
type screenRequest struct {
	model  tea.Model
	result chan tea.Model
}
type screenMessage struct {
	generation uint64
	message    tea.Msg
}
type screenSessionModel struct {
	child      tea.Model
	result     chan tea.Model
	generation uint64
	size       tea.WindowSizeMsg
	frame      tea.View
}

func (m *screenSessionModel) Init() tea.Cmd  { return nil }
func (m *screenSessionModel) View() tea.View { return m.frame }

func (m *screenSessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case screenRequest:
		m.generation++
		m.child, m.result = msg.model, msg.result
		init := m.child.Init()
		var resize tea.Cmd
		if m.size.Width > 0 {
			m.child, resize = m.child.Update(m.size)
		}
		m.frame = m.child.View()
		return m, tea.Batch(screenCommand(m.generation, init), screenCommand(m.generation, resize))
	case tea.KeyPressMsg:
		if m.child == nil && msg.String() == "ctrl+c" {
			return m, tea.Interrupt
		}
	case tea.WindowSizeMsg:
		m.size = msg
	case screenMessage:
		if msg.generation != m.generation || m.result == nil {
			return m, nil
		}
		switch value := msg.message.(type) {
		case tea.QuitMsg:
			// Freeze the frame before handing the child back to its caller.
			m.frame = m.child.View()
			m.result <- m.child
			m.result = nil
			m.child = nil
			return m, nil
		case tea.BatchMsg:
			commands := make([]tea.Cmd, len(value))
			for i, cmd := range value {
				commands[i] = screenCommand(m.generation, cmd)
			}
			return m, tea.Batch(commands...)
		}
		return m.updateChild(msg.message)
	}
	return m.updateChild(msg)
}

func (m *screenSessionModel) updateChild(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.child == nil || m.result == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.child, cmd = m.child.Update(msg)
	m.frame = m.child.View()
	return m, screenCommand(m.generation, cmd)
}

// Tag asynchronous results so a late filter, blink, or spinner tick cannot
// update a later screen. Quit is handled here as child completion, not exit.
func screenCommand(generation uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return screenMessage{generation, cmd()} }
}

type screenSession struct {
	program *tea.Program
	done    chan struct{}
	err     error // published by closing done
}

var screens struct {
	sync.Mutex
	session *screenSession
}

// ScreensActive reports whether the shared renderer still owns the terminal,
// including the gaps between screens. Terminal resets and console banners must
// not write over its frame or change its cursor mode.
func ScreensActive() bool {
	screens.Lock()
	defer screens.Unlock()
	if screens.session == nil {
		return false
	}
	select {
	case <-screens.session.done:
		return false
	default:
		return true
	}
}

// CloseScreens releases the shared renderer before another terminal UI or exit.
// Calls are safe when no session is running.
func CloseScreens() {
	screens.Lock()
	defer screens.Unlock()
	if s := screens.session; s != nil {
		s.program.Quit()
		<-s.done
		screens.session = nil
	}
}

func runScreen(model tea.Model) (tea.Model, error) {
	// Preserve the standalone failure/fallback behavior used by non-TTY callers.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		var final tea.Model
		err := RunClean(func() error {
			return busy(func() error {
				var err error
				final, err = NewProgram(model).Run()
				return err
			})
		})
		return final, err
	}
	screens.Lock()
	s := screens.session
	if s != nil {
		select {
		case <-s.done:
			screens.session = nil
			screens.Unlock()
			if s.err != nil {
				return nil, s.err
			}
			return nil, ErrPickCancelled
		default:
		}
	}
	if s == nil {
		s = &screenSession{done: make(chan struct{})}
		s.program = tea.NewProgram(&screenSessionModel{}, BubbleTeaProgramOptions()...)
		screens.session = s
		go func() {
			_, s.err = s.program.Run()
			close(s.done)
		}()
	}
	screens.Unlock()
	request := screenRequest{model: model, result: make(chan tea.Model, 1)}
	var final tea.Model
	err := busy(func() error {
		s.program.Send(request)
		select {
		case final = <-request.result:
			return nil
		case <-s.done:
			if s.err != nil {
				return s.err
			}
			return ErrPickCancelled
		}
	})
	return final, err
}
