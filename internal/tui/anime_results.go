package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/charmbracelet/x/ansi"
)

var (
	// ErrSelectionBack means the user requested the previous screen.
	ErrSelectionBack = errors.New("back from anime selection")
	// ErrSelectionCancelled means the user quit the result screen.
	ErrSelectionCancelled = errors.New("anime selection cancelled")
	// ErrNoAnimeResults prevents opening an empty result screen.
	ErrNoAnimeResults = errors.New("no anime results to select")
)

type animeResultItem struct {
	anime *models.Anime
}

// FilterValue returns all useful searchable metadata for an anime result.
func (i animeResultItem) FilterValue() string {
	if i.anime == nil {
		return ""
	}
	return strings.Join([]string{
		singleLine(i.anime.Name),
		singleLine(i.anime.Source),
		singleLine(i.anime.Year),
		singleLine(string(i.anime.MediaType)),
		singleLine(i.anime.Quality),
	}, " ")
}

// Title returns the primary result label.
func (i animeResultItem) Title() string {
	if i.anime == nil {
		return "Unknown title"
	}
	title := singleLine(i.anime.Name)
	if title == "" {
		return "Unknown title"
	}
	return title
}

// Description returns compact source, year, type, and quality metadata.
func (i animeResultItem) Description() string {
	if i.anime == nil {
		return "Information unavailable"
	}
	parts := make([]string, 0, 2)
	for _, value := range []string{i.anime.Source, i.anime.Year} {
		if value = singleLine(value); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "Information unavailable"
	}
	return strings.Join(parts, "  •  ")
}

// SingleLine strips terminal controls and collapses metadata to one line.
// Exported so callers can sanitize titles before building breadcrumb trails.
func SingleLine(value string) string {
	return singleLine(value)
}

// singleLine strips terminal controls and collapses metadata to one line.
func singleLine(value string) string {
	value = ansi.Strip(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

type animeResultsModel struct {
	theme         Theme
	shell         Shell
	results       list.Model
	selected      *models.Anime
	err           error
	filterPending bool
}

// newAnimeResultsModel creates the styled, filterable anime result screen.
func newAnimeResultsModel(animes []*models.Anime) *animeResultsModel {
	theme := NewTheme(true)
	shell := NewShell(&theme, "Search › Results")
	items := make([]list.Item, 0, len(animes))
	for _, anime := range animes {
		if anime == nil {
			continue
		}
		items = append(items, animeResultItem{anime: anime})
	}

	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(1)
	delegate.Styles.NormalTitle = theme.Text.PaddingLeft(2)
	delegate.Styles.NormalDesc = theme.Muted.PaddingLeft(2)
	delegate.Styles.SelectedTitle = theme.SelectedTitle
	delegate.Styles.SelectedDesc = theme.SelectedDescription
	delegate.Styles.DimmedTitle = theme.Muted.PaddingLeft(2)
	delegate.Styles.DimmedDesc = theme.Muted.Faint(true).PaddingLeft(2)
	delegate.Styles.FilterMatch = theme.FilterMatch

	width, height := shell.ContentSize()
	results := list.New(items, delegate, width, height)
	results.SetShowTitle(false)
	results.SetShowHelp(false)
	results.SetStatusBarItemName("result", "results")
	results.FilterInput.Prompt = "Filter: "
	results.Styles.StatusBar = theme.Muted
	results.Styles.StatusEmpty = theme.Muted
	results.Styles.StatusBarActiveFilter = theme.Primary
	results.Styles.StatusBarFilterCount = theme.Muted
	results.Styles.NoItems = theme.Muted
	results.Styles.PaginationStyle = theme.Muted
	results.Styles.ActivePaginationDot = theme.Primary
	results.Styles.InactivePaginationDot = theme.Muted
	results.Styles.Filter.Focused.Prompt = theme.Primary
	results.Styles.Filter.Focused.Text = theme.Text
	results.Styles.Filter.Focused.Placeholder = theme.Muted
	results.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	}

	return &animeResultsModel{theme: theme, shell: shell, results: results}
}

// Init starts the result screen without background work.
func (m *animeResultsModel) Init() tea.Cmd {
	return nil
}

// Update handles resize, filtering, navigation, selection, and cancellation.
func (m *animeResultsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(list.FilterMatchesMsg); ok {
		m.filterPending = false
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.shell.Resize(msg.Width, msg.Height)
		m.results.SetSize(m.shell.ContentSize())
		return m, nil

	case tea.KeyPressMsg:
		filtering := m.results.FilterState() == list.Filtering
		if m.shell.Logs {
			switch msg.String() {
			case LogsToggleKey, "esc", "q", "enter":
				m.shell.Logs = false
			case "ctrl+c":
				m.err = ErrSelectionCancelled
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			m.err = ErrSelectionCancelled
			return m, tea.Quit
		case LogsToggleKey:
			m.shell.ToggleLogs()
			return m, nil
		case "q":
			if !filtering {
				m.err = ErrSelectionCancelled
				return m, tea.Quit
			}
		case "esc":
			if m.results.FilterState() == list.Unfiltered {
				m.err = ErrSelectionBack
				return m, tea.Quit
			}
		case "enter":
			if !filtering {
				if item, ok := m.results.SelectedItem().(animeResultItem); ok && item.anime != nil {
					m.selected = item.anime
					return m, tea.Quit
				}
			} else if m.filterPending {
				return m, nil
			}
		}
	}

	filterBefore := m.results.FilterValue()
	var cmd tea.Cmd
	m.results, cmd = m.results.Update(msg)
	if m.results.FilterState() == list.Filtering && filterBefore != m.results.FilterValue() {
		m.filterPending = true
	}
	return m, cmd
}

// View renders the full-width result list inside the shell chrome. There is
// no side panel: every fact a source gives (title, source) is already on the
// row, and a panel only cost list width — long titles were clipped at 32
// columns on a 100-column terminal.
func (m *animeResultsModel) View() tea.View {
	view := tea.NewView(m.shell.Render(m.results.View(), "↑↓ move · / filter · enter open · esc back"))
	view.AltScreen = true
	view.WindowTitle = "GonimeId - Results"
	return view
}

type animeResultsRunner func(tea.Model) (tea.Model, error)

// SelectAnime opens the result screen and returns the chosen anime.
func SelectAnime(animes []*models.Anime) (*models.Anime, error) {
	return selectAnimeWithRunner(animes, func(model tea.Model) (tea.Model, error) {
		var final tea.Model
		err := RunClean(func() error {
			return busy(func() error {
				var runErr error
				final, runErr = NewProgram(model).Run()
				return runErr
			})
		})
		return final, err
	})
}

// selectAnimeWithRunner isolates terminal execution for deterministic tests.
func selectAnimeWithRunner(animes []*models.Anime, run animeResultsRunner) (*models.Anime, error) {
	valid := make([]*models.Anime, 0, len(animes))
	for _, anime := range animes {
		if anime != nil {
			valid = append(valid, anime)
		}
	}
	if len(valid) == 0 {
		return nil, ErrNoAnimeResults
	}
	if run == nil {
		return nil, fmt.Errorf("anime result runner not configured")
	}

	final, err := run(newAnimeResultsModel(valid))
	if err != nil {
		return nil, fmt.Errorf("run anime result screen: %w", err)
	}
	model, ok := final.(*animeResultsModel)
	if !ok || model == nil {
		return nil, fmt.Errorf("unexpected anime result model %T", final)
	}
	if model.err != nil {
		return nil, model.err
	}
	if model.selected == nil {
		return nil, ErrSelectionCancelled
	}
	return model.selected, nil
}
