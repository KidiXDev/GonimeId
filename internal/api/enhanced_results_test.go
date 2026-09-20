package api

import (
	"context"
	"errors"
	"testing"

	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchAnimeEnhancedCore_ResultScreenCascade(t *testing.T) {
	t.Parallel()

	t.Run("search select enrich", func(t *testing.T) {
		t.Parallel()
		first := &models.Anime{Name: "Frieren", Source: "Otakudesu", URL: "https://otakudesu.blog/anime/frieren/"}
		second := &models.Anime{Name: "Frieren", Source: "Samehadaku", URL: "https://v2.samehadaku.how/anime/frieren/", Year: "2023"}
		providerResults := []*models.Anime{first, nil, second}
		var selectedInput []*models.Anime
		var gotKinds []source.SourceKind
		search := func(_ context.Context, query string, kinds []source.SourceKind) ([]*models.Anime, error) {
			assert.Equal(t, "frieren", query)
			gotKinds = kinds
			return providerResults, nil
		}
		selectAnime := func(animes []*models.Anime) (*models.Anime, error) {
			selectedInput = animes
			return animes[1], nil
		}
		enrich := func(anime *models.Anime) error {
			anime.ImageURL = "mock://cover"
			return nil
		}

		selected, results, err := searchAnimeEnhancedWithResults("frieren", "otakudesu", search, selectAnime, enrich)
		require.NoError(t, err)
		assert.Equal(t, []*models.Anime{first, second}, selectedInput, "nil results are dropped, order is kept")
		assert.Equal(t, []*models.Anime{first, second}, results)
		assert.Equal(t, []source.SourceKind{source.Otakudesu}, gotKinds)
		assert.Same(t, second, selected)
		assert.Equal(t, "mock://cover", selected.ImageURL)
		assert.Equal(t, []*models.Anime{first, nil, second}, providerResults, "provider-owned slices are not reordered")
	})

	t.Run("source selector maps exact kinds", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			src  string
			want []source.SourceKind
		}{
			{src: "otakudesu", want: []source.SourceKind{source.Otakudesu}},
			{src: " Samehadaku ", want: []source.SourceKind{source.Samehadaku}},
			{src: "nimegami", want: []source.SourceKind{source.Nimegami}},
			{src: "ylnime", want: []source.SourceKind{source.Ylnime}},
			{src: "moenime", want: []source.SourceKind{source.Moenime}},
			{src: "unknown", want: nil},
			{src: "", want: nil},
		}
		for _, tt := range tests {
			t.Run(tt.src, func(t *testing.T) {
				anime := &models.Anime{Name: "Frieren", Source: "Existing"}
				var got []source.SourceKind
				search := func(_ context.Context, _ string, kinds []source.SourceKind) ([]*models.Anime, error) {
					got = kinds
					return []*models.Anime{anime}, nil
				}
				selected, err := searchAnimeEnhanced("frieren", tt.src, search, func([]*models.Anime) (*models.Anime, error) {
					return anime, nil
				}, nil)
				require.NoError(t, err)
				assert.Same(t, anime, selected)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("errors and back navigation", func(t *testing.T) {
		t.Parallel()
		anime := &models.Anime{Name: "Frieren"}
		ok := func(context.Context, string, []source.SourceKind) ([]*models.Anime, error) {
			return []*models.Anime{anime}, nil
		}

		_, err := searchAnimeEnhanced("frieren", "", nil, nil, nil)
		require.ErrorContains(t, err, "search dispatch not wired")

		_, err = searchAnimeEnhanced("frieren", "", func(context.Context, string, []source.SourceKind) ([]*models.Anime, error) {
			return nil, errors.New("boom")
		}, nil, nil)
		require.ErrorContains(t, err, "boom")

		_, err = searchAnimeEnhanced("frieren", "", func(context.Context, string, []source.SourceKind) ([]*models.Anime, error) {
			return []*models.Anime{nil}, nil
		}, nil, nil)
		require.ErrorContains(t, err, "no results found")

		_, err = searchAnimeEnhanced("frieren", "", ok, nil, nil)
		require.ErrorContains(t, err, "anime selection not configured")

		_, err = searchAnimeEnhanced("frieren", "", ok, func([]*models.Anime) (*models.Anime, error) {
			return nil, tui.ErrSelectionBack
		}, nil)
		require.ErrorIs(t, err, ErrBackToSearch)

		_, err = searchAnimeEnhanced("frieren", "", ok, func([]*models.Anime) (*models.Anime, error) {
			return nil, errors.New("cancelled")
		}, nil)
		require.ErrorContains(t, err, "anime selection cancelled")

		_, err = searchAnimeEnhanced("frieren", "", ok, func([]*models.Anime) (*models.Anime, error) {
			return nil, nil
		}, nil)
		require.ErrorContains(t, err, "anime selection returned nil")

		selected, err := searchAnimeEnhanced("frieren", "", ok, func([]*models.Anime) (*models.Anime, error) {
			return anime, nil
		}, func(*models.Anime) error { return errors.New("enrich failed") })
		require.NoError(t, err, "enrichment failure is a warning, not an error")
		assert.Same(t, anime, selected)
	})
}
