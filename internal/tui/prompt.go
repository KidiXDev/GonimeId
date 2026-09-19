package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// PromptOptions configure a single-line text prompt screen.
type PromptOptions struct {
	Breadcrumb  string // shell navigation trail, e.g. "Search"
	Title       string // question above the input
	Placeholder string
	MinLength   int // shortest accepted answer (after trimming)
}

type promptModel struct {
	shell  Shell
	theme  Theme
	opts   PromptOptions
	input  textinput.Model
	value  string
	err    error
	hint   string
	submit bool
}

func newPromptModel(opts PromptOptions) *promptModel {
	theme := NewTheme(true)
	in := textinput.New()
	in.Placeholder = opts.Placeholder
	in.Prompt = "❯ "
	in.Focus()
	styles := in.Styles()
	styles.Focused.Prompt = theme.Primary
	styles.Focused.Text = theme.Text
	styles.Focused.Placeholder = theme.Muted
	in.SetStyles(styles)
	return &promptModel{shell: NewShell(&theme, singleLine(opts.Breadcrumb)), theme: theme, opts: opts, input: in}
}

func (m *promptModel) Init() tea.Cmd { return textinput.Blink }

func (m *promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.shell.Resize(msg.Width, msg.Height)
		w, _ := m.shell.ContentSize()
		m.input.SetWidth(max(w-6, 10))
		return m, nil
	case tea.KeyPressMsg:
		if m.shell.Logs && msg.String() != "ctrl+c" {
			switch msg.String() {
			case LogsToggleKey, "esc", "q", "enter":
				m.shell.Logs = false
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			m.err = ErrPickCancelled
			return m, tea.Quit
		case "esc":
			if m.shell.Logs {
				m.shell.Logs = false
				return m, nil
			}
			m.err = ErrPickBack
			return m, tea.Quit
		case LogsToggleKey:
			m.shell.ToggleLogs()
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if len(value) < m.opts.MinLength {
				m.hint = fmt.Sprintf("Type at least %d characters", m.opts.MinLength)
				return m, nil
			}
			m.value, m.submit = value, true
			return m, tea.Quit
		}
		m.hint = ""
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *promptModel) View() tea.View {
	lines := []string{"", "  " + m.theme.Value.Render(m.opts.Title), "", "  " + m.input.View()}
	if m.hint != "" {
		lines = append(lines, "", "  "+m.theme.Warn.Render(m.hint))
	}
	view := tea.NewView(m.shell.Render(strings.Join(lines, "\n"), "enter search · esc back"))
	view.AltScreen = true
	view.WindowTitle = "GonimeId - " + m.opts.Title
	return view
}

// Prompt asks for one line of text on a full shell screen. Esc is ErrPickBack,
// ctrl+c is ErrPickCancelled.
func Prompt(opts PromptOptions) (string, error) {
	return promptWithRunner(opts, runScreen)
}

type promptRunner func(tea.Model) (tea.Model, error)

func promptWithRunner(opts PromptOptions, run promptRunner) (string, error) {
	if opts.Title == "" {
		opts.Title = "Type title"
	}
	final, err := run(newPromptModel(opts))
	if err != nil {
		return "", fmt.Errorf("run prompt screen: %w", err)
	}
	model, ok := final.(*promptModel)
	if !ok || model == nil {
		return "", fmt.Errorf("unexpected prompt model %T", final)
	}
	if model.err != nil {
		return "", model.err
	}
	if !model.submit {
		return "", ErrPickCancelled
	}
	return model.value, nil
}
