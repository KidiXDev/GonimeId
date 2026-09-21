package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/KidiXDev/GonimeId/internal/tracking"
)

// HomeAction describes what the watch hub asked the application to do.
type HomeAction int

const (
	HomeQuit HomeAction = iota
	HomeSearch
	HomeOpenSeries
	HomeToggleAutoplay
	HomeDeleteSeries
	HomeClearHistory
)

// HomeResult is the selected hub action and title, when one is required.
type HomeResult struct {
	Action HomeAction
	Series tracking.Series
}

type homeEntry struct {
	series  tracking.Series
	success lipgloss.Style
}

func (e homeEntry) FilterValue() string { return e.series.Title + " " + e.series.Source }
func (e homeEntry) Title() string       { return singleLine(e.series.Title) }
func (e homeEntry) Description() string {
	latest, ok := latestEpisode(e.series)
	if !ok {
		return "No episode progress"
	}
	status := fmt.Sprintf("Episode %d", latest.EpisodeNumber)
	if latest.Completed {
		status += " ✓"
	} else if percent := latest.ProgressPercent(); percent > 0 {
		status += fmt.Sprintf(" · %d%% watched", percent)
	} else {
		status += " · Not started"
	}
	if e.series.Source != "" {
		status += " · " + e.series.Source
	}
	if latest.Completed {
		return e.success.Render(status)
	}
	return status
}

type homeModel struct {
	theme       Theme
	shell       Shell
	all         []tracking.Series
	entries     list.Model
	tab         int
	autoplay    bool
	tracking    bool
	result      HomeResult
	err         error
	wide        bool
	listWidth   int
	detailWidth int
}

func newHomeModel(series []tracking.Series, autoplay, trackingAvailable bool) *homeModel {
	theme := NewTheme(true)
	shell := NewShell(&theme, "Watch Hub")
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.ShowDescription = true
	delegate.Styles.NormalTitle = theme.Text.PaddingLeft(2)
	delegate.Styles.NormalDesc = theme.Muted.PaddingLeft(4)
	delegate.Styles.SelectedTitle = theme.SelectedTitle
	delegate.Styles.SelectedDesc = theme.SelectedDescription.PaddingLeft(1)
	delegate.Styles.FilterMatch = theme.FilterMatch

	width, height := shell.ContentSize()
	entries := list.New(nil, delegate, width, max(height-2, 1))
	entries.KeyMap = fancyListKeyMap()
	entries.DisableQuitKeybindings()
	entries.SetShowTitle(false)
	entries.SetShowHelp(false)
	entries.SetStatusBarItemName("title", "titles")
	entries.FilterInput.Prompt = "❯ "
	entries.FilterInput.Placeholder = "filter titles…"
	entries.Styles.StatusBar = theme.Muted
	entries.Styles.StatusEmpty = theme.Muted
	entries.Styles.StatusBarActiveFilter = theme.Primary
	entries.Styles.NoItems = theme.Muted
	entries.Styles.PaginationStyle = theme.Muted
	entries.Styles.ActivePaginationDot = theme.Primary
	entries.Styles.InactivePaginationDot = theme.Muted
	entries.Styles.Filter.Focused.Prompt = theme.Primary
	entries.Styles.Filter.Focused.Text = theme.Text
	entries.Styles.Filter.Focused.Placeholder = theme.Muted

	m := &homeModel{theme: theme, shell: shell, all: series, entries: entries, autoplay: autoplay, tracking: trackingAvailable}
	m.setTab(0)
	return m
}

func (m *homeModel) Init() tea.Cmd { return nil }

func (m *homeModel) setTab(tab int) {
	m.tab = (tab + 2) % 2
	items := make([]list.Item, 0, len(m.all))
	for _, series := range m.all {
		if m.tab == 0 && (series.Finished() || (series.MediaType == "movie" && len(series.Episodes) > 0 && series.Episodes[0].Completed)) {
			continue
		}
		items = append(items, homeEntry{series: series, success: m.theme.Success})
	}
	m.entries.SetItems(items)
	if len(items) > 0 {
		m.entries.Select(0)
	}
}

func (m *homeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.shell.Resize(msg.Width, msg.Height)
		width, height := m.shell.ContentSize()
		m.wide = width >= 92
		m.listWidth = width
		m.detailWidth = 0
		if m.wide {
			m.detailWidth = min(max(width/3, 28), 34)
			m.listWidth = width - m.detailWidth - 1
		}
		m.entries.SetSize(m.listWidth, max(height-2, 1))
		return m, nil
	case tea.KeyPressMsg:
		filtering := m.entries.FilterState() == list.Filtering
		if m.shell.Logs {
			switch msg.String() {
			case LogsToggleKey, "esc", "q", "enter":
				m.shell.Logs = false
			case "ctrl+c":
				m.result.Action = HomeQuit
				return m, tea.Quit
			}
			return m, nil
		}
		if !filtering {
			switch msg.String() {
			case "ctrl+c", "q":
				m.result.Action = HomeQuit
				return m, tea.Quit
			case LogsToggleKey:
				m.shell.ToggleLogs()
				return m, nil
			case "left", "h", "shift+tab":
				m.setTab(m.tab - 1)
				return m, nil
			case "right", "l", "tab":
				m.setTab(m.tab + 1)
				return m, nil
			case "s":
				m.result.Action = HomeSearch
				return m, tea.Quit
			case "a":
				if !m.tracking {
					return m, nil
				}
				m.result.Action = HomeToggleAutoplay
				return m, tea.Quit
			case "d":
				if !m.tracking {
					return m, nil
				}
				return m.quitWithSelected(HomeDeleteSeries)
			case "D":
				if !m.tracking {
					return m, nil
				}
				m.result.Action = HomeClearHistory
				return m, tea.Quit
			case "enter":
				return m.quitWithSelected(HomeOpenSeries)
			}
		}
	}

	var cmd tea.Cmd
	m.entries, cmd = m.entries.Update(msg)
	return m, cmd
}

func (m *homeModel) quitWithSelected(action HomeAction) (tea.Model, tea.Cmd) {
	entry, ok := m.entries.SelectedItem().(homeEntry)
	if !ok {
		return m, nil
	}
	m.result = HomeResult{Action: action, Series: entry.series}
	return m, tea.Quit
}

func (m *homeModel) View() tea.View {
	tabs := m.theme.Primary.Render("[Continue]") + "   " + m.theme.Muted.Render("Recent")
	if m.tab == 0 {
		tabs = m.theme.Primary.Render("[Continue]") + "   " + m.theme.Muted.Render("Recent")
	} else {
		tabs = m.theme.Muted.Render("Continue") + "   " + m.theme.Primary.Render("[Recent]")
	}
	listBody := m.entries.View()
	if len(m.entries.Items()) == 0 && m.entries.FilterState() == list.Unfiltered {
		empty := "Nothing to continue yet.\n\nPress s to search for an anime."
		if !m.tracking {
			empty = "Watch tracking is unavailable in this build.\n\nSearch and playback still work — press s to search."
		}
		if m.tab == 1 {
			empty = "Your watch history is empty.\n\nPress s to start watching."
			if !m.tracking {
				empty = "Watch tracking is unavailable in this build.\n\nSearch and playback still work — press s to search."
			}
		}
		listBody = lipgloss.NewStyle().Padding(2).Render(m.theme.Muted.Render(empty))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, tabs, "", listBody)
	if m.wide {
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(m.listWidth).Render(body), " ", m.detail())
	}
	autoplay := "off"
	if m.autoplay {
		autoplay = "on"
	}
	footer := "←→ tabs · s search · enter episodes · q quit"
	if m.tracking {
		footer = fmt.Sprintf("←→ tabs · s search · enter episodes · a autoplay %s · d remove · D clear · q quit", autoplay)
	}
	shell := m.shell
	shell.CompactFooter = homeCompactFooter(shell.Width)
	view := tea.NewView(shell.Render(body, footer))
	view.AltScreen = true
	view.WindowTitle = "GonimeId - Watch Hub"
	return view
}

func (m *homeModel) detail() string {
	entry, ok := m.entries.SelectedItem().(homeEntry)
	if !ok {
		return m.theme.Panel.Width(max(m.detailWidth-4, 1)).Render("Search for an anime to begin your watch history.\n\nPress s to search.")
	}
	series := entry.series
	latest, _ := latestEpisode(series)
	progress := fmt.Sprintf("Episode %d", latest.EpisodeNumber)
	if latest.Completed {
		progress = m.theme.Success.Render(progress + " ✓")
	} else if percent := latest.ProgressPercent(); percent > 0 {
		progress += fmt.Sprintf(" · %d%%", percent)
	} else {
		progress += " · Not started"
	}
	meta := fmt.Sprintf("%d tracked · %s", len(series.Episodes), relativeTime(series.LastUpdated))
	if series.Source != "" {
		meta += " · " + singleLine(series.Source)
	}
	lines := []string{
		m.theme.Value.Render(series.Title),
		"",
		m.theme.Primary.Render("Choose an episode"),
		progress,
		m.theme.Muted.Render(meta),
	}
	return m.theme.Panel.Width(m.detailWidth).Render(strings.Join(lines, "\n"))
}

func homeCompactFooter(width int) string {
	switch {
	case width >= 40:
		return "←→ tabs · enter episodes · s search"
	case width >= 28:
		return "enter episodes · s search"
	case width >= 18:
		return "enter · s search"
	default:
		return "enter · s"
	}
}

func latestEpisode(series tracking.Series) (tracking.Anime, bool) {
	if len(series.Episodes) == 0 {
		return tracking.Anime{}, false
	}
	latest := series.Episodes[0]
	for _, episode := range series.Episodes[1:] {
		if episode.LastUpdated.After(latest.LastUpdated) {
			latest = episode
		}
	}
	return latest, true
}

func relativeTime(at time.Time) string {
	if at.IsZero() {
		return "Unknown"
	}
	age := time.Since(at)
	switch {
	case age < time.Minute:
		return "Just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
}

// RunHome opens the watch hub and returns the requested application action.
func RunHome(series []tracking.Series, autoplay, trackingAvailable bool) (HomeResult, error) {
	final, err := runScreen(newHomeModel(series, autoplay, trackingAvailable))
	if err != nil {
		return HomeResult{}, fmt.Errorf("run watch hub: %w", err)
	}
	model, ok := final.(*homeModel)
	if !ok || model == nil {
		return HomeResult{}, fmt.Errorf("unexpected watch hub model %T", final)
	}
	if model.err != nil {
		return HomeResult{}, model.err
	}
	return model.result, nil
}

// Confirm asks for destructive-action confirmation using the shared picker.
func Confirm(question string) (bool, error) {
	index, err := PickLabels([]string{"Cancel", "Confirm"}, PickOptions{
		Breadcrumb: question, WindowTitle: "GonimeId - Confirm", InitialIndex: 0,
	})
	if err != nil {
		if errors.Is(err, ErrPickBack) || errors.Is(err, ErrPickCancelled) {
			return false, nil
		}
		return false, err
	}
	return index == 1, nil
}
