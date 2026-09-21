package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultShellWidth  = 80
	defaultShellHeight = 24
	shellChromeHeight  = 4
)

// Shell renders common GonimeId navigation around a screen body.
type Shell struct {
	Theme      Theme
	Breadcrumb string
	Width      int
	Height     int
	// Logs shows the session log overlay in place of the body (ctrl+l).
	Logs bool
	// CompactFooter overrides the generic picker legend on narrow screens.
	CompactFooter string
}

// noticeWindow is how long a WARN/ERROR stays in the footer after it happened.
const noticeWindow = 20 * time.Second

// LogsToggleKey is the key every screen binds to the session log overlay.
const LogsToggleKey = "ctrl+l"

// ToggleLogs flips the log overlay; screens call it on LogsToggleKey.
func (s *Shell) ToggleLogs() { s.Logs = !s.Logs }

// NewShell creates a shell with safe dimensions before the first resize event.
func NewShell(theme *Theme, breadcrumb string) Shell {
	return Shell{
		Theme:      *theme,
		Breadcrumb: breadcrumb,
		Width:      defaultShellWidth,
		Height:     defaultShellHeight,
	}
}

// Resize updates the available terminal dimensions.
func (s *Shell) Resize(width, height int) {
	s.Width = max(width, 1)
	s.Height = max(height, shellChromeHeight+1)
}

// ContentSize returns space left after header, separators, and footer.
func (s *Shell) ContentSize() (width, height int) {
	width = s.Width
	if width <= 0 {
		width = defaultShellWidth
	}
	height = s.Height
	if height <= 0 {
		height = defaultShellHeight
	}
	return width, max(height-shellChromeHeight, 1)
}

// Render wraps body content in responsive navigation chrome. With the log
// overlay open the body is replaced by the session log and the footer by the
// overlay's own legend; otherwise the newest recent WARN/ERROR is shown at the
// right of the footer so a silenced console message is never lost.
func (s *Shell) Render(body, footer string) string {
	width, height := s.ContentSize()
	crumb := s.Breadcrumb
	if s.Logs {
		crumb = "Logs"
		body = joinLines(formatLogLines(&s.Theme, RecentLogs(), width, height))
		footer = "ctrl+l / esc close · newest at the bottom"
	} else if notice, ok := LastNotice(noticeWindow); ok && width >= 50 {
		footer = withNotice(&s.Theme, footer, notice, width)
	}
	header := s.Theme.Header.Render("GONIMEID")
	if width >= 34 && crumb != "" {
		header += "  " + s.Theme.Breadcrumb.Render(crumb)
	}
	if width >= 60 && !s.Logs {
		hint := s.Theme.Muted.Render("ctrl+l logs")
		if pad := width - ansi.StringWidth(header) - ansi.StringWidth(hint); pad > 1 {
			header += strings.Repeat(" ", pad) + hint
		}
	}

	if width < 50 {
		if s.CompactFooter != "" {
			footer = s.CompactFooter
		} else {
			switch {
			case width >= 36:
				footer = "type fzf  ↑↓/jk  enter  esc"
			case width >= 24:
				footer = "type  ↑↓  enter  esc"
			case width >= 12:
				footer = "type  enter  esc"
			case width >= 8:
				footer = "esc back"
			default:
				footer = "esc"
			}
		}
	}
	separator := s.Theme.Border.Render(strings.Repeat("─", width))
	return lipgloss.JoinVertical(lipgloss.Left,
		fitBlock(header, width, 1),
		separator,
		lipgloss.NewStyle().Height(height).Render(fitBlock(body, width, height)),
		separator,
		fitBlock(s.Theme.Footer.Render(footer), width, 1),
	)
}

// withNotice appends a WARN/ERROR line to the right of the footer legend,
// truncated so the legend keeps its space.
func withNotice(theme *Theme, footer string, notice LogEntry, width int) string {
	style := theme.Warn
	if notice.Level == LogError {
		style = theme.Error
	}
	room := width - ansi.StringWidth(footer) - 3
	if room < 12 {
		return footer
	}
	text := ansi.Truncate(notice.Message, room, "…")
	pad := width - ansi.StringWidth(footer) - ansi.StringWidth(text)
	return footer + strings.Repeat(" ", max(pad, 1)) + style.Render(text)
}

// fitBlock clips ANSI-styled content to terminal cell dimensions.
func fitBlock(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	content = strings.ToValidUTF8(content, "�")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	return strings.Join(lines, "\n")
}
