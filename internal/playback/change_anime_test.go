package playback

import (
	"context"
	"testing"

	"github.com/KidiXDev/GonimeId/internal/api"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestChangeAnimeBackDoesNotExhaustRetries(t *testing.T) {
	calls := 0
	anime := &models.Anime{Name: "Naruto"}
	got, episodes, err := changeAnimeWith(func(opts tui.PromptOptions) (string, error) {
		require.Equal(t, "Player › Change anime", opts.Breadcrumb)
		require.Equal(t, 2, opts.MinLength)
		return "Naruto", nil
	}, func(string, string) (*models.Anime, error) {
		calls++
		if calls <= 4 {
			return nil, api.ErrBackToSearch
		}
		return anime, nil
	}, func(context.Context, *models.Anime) ([]models.Episode, error) {
		return []models.Episode{{Number: "1"}}, nil
	})
	require.NoError(t, err)
	require.Same(t, anime, got)
	require.Len(t, episodes, 1)
	require.Equal(t, 5, calls)
}

func TestChangeAnimeCancellationAndEmptyEpisodes(t *testing.T) {
	t.Run("cancel results", func(t *testing.T) {
		calls := 0
		_, _, err := changeAnimeWith(func(tui.PromptOptions) (string, error) { calls++; return "Naruto", nil }, func(string, string) (*models.Anime, error) { return nil, tui.ErrSelectionCancelled }, func(context.Context, *models.Anime) ([]models.Episode, error) {
			t.Fatal("fetch after cancellation")
			return nil, nil
		})
		require.ErrorIs(t, err, tui.ErrSelectionCancelled)
		require.Equal(t, 1, calls)
	})
	t.Run("empty episodes", func(t *testing.T) {
		calls := 0
		anime, episodes, err := changeAnimeWith(func(tui.PromptOptions) (string, error) { return "Naruto", nil }, func(string, string) (*models.Anime, error) { return &models.Anime{Name: "Naruto"}, nil }, func(context.Context, *models.Anime) ([]models.Episode, error) { calls++; return nil, nil })
		require.Error(t, err)
		require.Nil(t, anime)
		require.Empty(t, episodes)
		require.Equal(t, 3, calls)
	})
}
