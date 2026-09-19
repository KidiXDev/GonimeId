package gonimeid_test

import (
	"errors"
	"testing"

	"github.com/KidiXDev/GonimeId/pkg/gonimeid"
	"github.com/KidiXDev/GonimeId/pkg/gonimeid/types"
)

func TestNewClient(t *testing.T) {
	client := gonimeid.NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestGetAvailableSources(t *testing.T) {
	client := gonimeid.NewClient()
	sources := client.GetAvailableSources()

	if len(sources) == 0 {
		t.Fatal("No sources available")
	}

	// Both live sources must be listed.
	seen := map[types.Source]bool{}
	for _, source := range sources {
		seen[source] = true
	}
	for _, want := range []types.Source{types.SourceOtakudesu, types.SourceSamehadaku, types.SourceNimegami} {
		if !seen[want] {
			t.Errorf("%s source not found", want)
		}
	}
}

func TestSourceString(t *testing.T) {
	tests := []struct {
		source   types.Source
		expected string
	}{
		{types.SourceOtakudesu, "Otakudesu"},
		{types.SourceSamehadaku, "Samehadaku"},
		{types.SourceNimegami, "Nimegami"},
	}

	for _, tt := range tests {
		if got := tt.source.String(); got != tt.expected {
			t.Errorf("Source.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		input    string
		expected types.Source
		hasError bool
	}{
		{"Otakudesu", types.SourceOtakudesu, false},
		{"samehadaku", types.SourceSamehadaku, false},
		{" Samehadaku ", types.SourceSamehadaku, false},
		{"invalid", types.SourceOtakudesu, true},
	}

	for _, tt := range tests {
		got, err := types.ParseSource(tt.input)
		if tt.hasError {
			if err == nil {
				t.Errorf("ParseSource(%q) expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseSource(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseSource(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		}
	}
}

// Integration test - requires network access
func TestSearchAnime_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := gonimeid.NewClient()

	// Test search across all sources
	results, err := client.SearchAnime("Naruto", nil)
	if err != nil {
		if errors.Is(err, gonimeid.ErrSourceUnavailable) {
			t.Skipf("Skipping integration test while upstream source is unavailable: %v", err)
		}
		t.Fatalf("SearchAnime failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchAnime returned no results")
	}

	// Verify result structure
	anime := results[0]
	if anime.Name == "" {
		t.Error("Anime name is empty")
	}
	if anime.URL == "" {
		t.Error("Anime URL is empty")
	}
	if anime.Source == "" {
		t.Error("Anime source is empty")
	}
}

// Integration test for specific source
func TestSearchAnimeSpecificSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := gonimeid.NewClient()
	source := types.SourceOtakudesu

	results, err := client.SearchAnime("One Piece", &source)
	if err != nil {
		if errors.Is(err, gonimeid.ErrSourceUnavailable) {
			t.Skipf("Skipping source-specific integration test while upstream source is unavailable: %v", err)
		}
		t.Fatalf("SearchAnime with specific source failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchAnime returned no results for specific source")
	}

	// All results should be from the specified source
	for _, anime := range results {
		if anime.Source != source.String() {
			t.Errorf("Expected source %s, got %s", source.String(), anime.Source)
		}
	}
}
