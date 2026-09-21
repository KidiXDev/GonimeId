package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/KidiXDev/GonimeId/internal/tracking"
)

func TestHomeModelActionsAndResponsiveView(t *testing.T) {
	series := tracking.Series{
		Key: "anilist:1", URL: "show", Title: "A Show", ContinueEpisode: 2, LastUpdated: time.Now(),
		Episodes: []tracking.Anime{{EpisodeNumber: 1, Completed: true, Duration: 100, PlaybackTime: 100, LastUpdated: time.Now()}},
	}
	m := newHomeModel([]tracking.Series{series}, true, true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(*homeModel)
	if !m.wide || m.View().Content == "" {
		t.Fatal("wide watch hub did not render")
	}
	if !strings.Contains(m.View().Content, "[Continue]") {
		t.Fatal("active tab lacks a textual marker")
	}
	detail := m.detail()
	if m.detailWidth > 34 || lipgloss.Width(detail) != m.detailWidth {
		t.Fatalf("detail panel width = %d, rendered = %d", m.detailWidth, lipgloss.Width(detail))
	}
	if !strings.Contains(detail, "Episode 1 ✓") || strings.Contains(detail, "Completed") {
		t.Fatalf("completed state is not compact and inline: %q", detail)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*homeModel)
	if cmd == nil || m.result.Action != HomeOpenSeries || m.result.Series.Key != series.Key {
		t.Fatalf("unexpected episode selector action: %+v", m.result)
	}
}

func TestHomeEnterOpensEpisodeSelectorFromBothTabs(t *testing.T) {
	series := tracking.Series{Key: "show", Title: "Show", Episodes: []tracking.Anime{{EpisodeNumber: 1}}}
	for tab := range 2 {
		m := newHomeModel([]tracking.Series{series}, true, true)
		m.setTab(tab)
		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		result := updated.(*homeModel).result
		if cmd == nil || result.Action != HomeOpenSeries {
			t.Fatalf("tab %d action = %v", tab, result.Action)
		}
	}
}

func TestHomeEmptyStateIsActionableAtNarrowWidth(t *testing.T) {
	m := newHomeModel(nil, true, true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	view := updated.(*homeModel).View().Content
	if !strings.Contains(view, "Nothing to continue") || !strings.Contains(view, "s search") {
		t.Fatalf("narrow empty state is not actionable: %q", view)
	}
}

func TestCompactFootersKeepPrimaryActionsVisible(t *testing.T) {
	for _, width := range []int{32, 16} {
		home := homeCompactFooter(width)
		if !strings.Contains(home, "enter") || !strings.Contains(home, "s") {
			t.Fatalf("home footer at %d columns: %q", width, home)
		}
		picker := toggleCompactFooter(width, "m")
		if !strings.Contains(picker, "enter") || !strings.Contains(picker, "m") {
			t.Fatalf("picker footer at %d columns: %q", width, picker)
		}
	}
}

func TestPickerExternalEventClosesScreen(t *testing.T) {
	events := make(chan string, 1)
	events <- "eof"
	m := newPickerModel([]PickItem{{Label: "Next episode"}}, PickOptions{External: events})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("external picker did not subscribe")
	}
	updated, quit := m.Update(cmd())
	m = updated.(*pickerModel)
	if quit == nil || m.externalEvent != "eof" {
		t.Fatalf("external event not captured: %q", m.externalEvent)
	}
}

func TestCountdownCompletesAndCancels(t *testing.T) {
	m := newCountdownModel("Episode 2", 1)
	updated, cmd := m.Update(countdownTick{})
	m = updated.(*countdownModel)
	if cmd == nil || !m.advance {
		t.Fatal("countdown did not advance at zero")
	}

	m = newCountdownModel("Episode 2", 5)
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(*countdownModel)
	if cmd == nil || m.advance {
		t.Fatal("escape did not cancel countdown")
	}
}
