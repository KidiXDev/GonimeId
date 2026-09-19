package appflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KidiXDev/GonimeId/internal/api"
	"github.com/KidiXDev/GonimeId/internal/api/providers"

	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/KidiXDev/GonimeId/internal/util"
)

// Injectable package-level dependencies. Tests swap these via the
// helpers in *_test.go (which serialise on appflowOverrideMu). Production
// callers never touch them.
var (
	// searchEnhancedFn is the underlying search implementation.
	searchEnhancedFn = api.SearchAnimeEnhanced

	// searchWithRetryFn is the per-attempt search used inside SearchAnimeWithRetry.
	searchWithRetryFn = api.SearchAnimeEnhanced

	// aniListFetchFn fetches AniList metadata for an anime title.
	aniListFetchFn = api.FetchAnimeFromAniList

	// sourceDetailsFetchFn enriches anime with provider-specific details.
	sourceDetailsFetchFn = api.FetchAnimeDetails

	// getAnimeEpisodesEnhancedFn returns the episode list for an anime. It now
	// dispatches through the Model B registry (providers.FetchEpisodes) instead
	// of api's legacy per-source switch.
	getAnimeEpisodesEnhancedFn = func(anime *models.Anime) ([]models.Episode, error) {
		return providers.FetchEpisodes(context.Background(), anime)
	}

	// getAnimeEpisodesLegacyFn returns episodes by URL (legacy API).
	getAnimeEpisodesLegacyFn = api.GetAnimeEpisodes

	// runSpinnerFn wraps a long action in a TUI spinner. Default uses
	// huh.spinner + tui.RunClean. Tests inject a synchronous passthrough.
	runSpinnerFn = defaultRunSpinner

	// promptForNameFn asks the user for a new search name. Default uses
	// huh.NewInput inside tui.RunClean. Tests inject a scripted sequence.
	promptForNameFn = defaultPromptForName
)

// defaultRunSpinner is the production spinner wrapper. It is replaced by
// tests with a synchronous passthrough so the action runs inline.
//
// The spinner is decoration; the action is the work. When there is no terminal
// (tui.ErrNoTTY) or the spinner itself fails, the action has not run, so it is
// run directly here — a non-interactive environment must still make progress.
// sync.Once makes the two paths mutually exclusive, so an action that already
// ran inside the spinner is never repeated.
func defaultRunSpinner(title string, action func()) {
	_ = tui.RunLoading("Search › Results", title, func(context.Context) error {
		action()
		return nil
	})
}

// defaultPromptForName is the production prompt. Returns the user's input
// trimmed, or an error if cancelled / empty / TTY unavailable.
func defaultPromptForName(_ string) (string, error) {
	name, err := tui.Prompt(tui.PromptOptions{
		Breadcrumb:  "Search",
		Title:       "Search anime",
		Placeholder: "Type title",
		MinLength:   2,
	})
	if err != nil {
		return "", fmt.Errorf("search cancelled by user: %w", err)
	}
	return name, nil
}

// SearchAnime searches for an anime by name using the globally configured source.
func SearchAnime(name string) (*models.Anime, error) {
	searchStart := time.Now()

	// Use enhanced API with source selection (spinner is inside api.SearchAnimeEnhanced)
	anime, err := searchEnhancedFn(name, util.GlobalSource)
	if err != nil {
		return nil, fmt.Errorf("failed to search for anime: %w", err)
	}

	util.Debugf("[PERF] SearchAnime completed in %v", time.Since(searchStart))
	return anime, nil
}

// SearchAnimeEnhanced - busca em todas as fontes registradas simultaneamente
func SearchAnimeEnhanced(name string) (*models.Anime, error) {
	searchStart := time.Now()

	// Buscar em ambas as fontes (spinner is inside api.SearchAnimeEnhanced)
	anime, err := searchEnhancedFn(name, "")
	if err != nil {
		return nil, fmt.Errorf("failed to search for anime: %w", err)
	}

	util.Debugf("[PERF] SearchAnimeEnhanced completed in %v", time.Since(searchStart))
	return anime, nil
}

// SearchAnimeWithRetry - searches for anime with retry logic on failure
func SearchAnimeWithRetry(name string) (*models.Anime, error) {
	currentName := name

	for {
		searchStart := time.Now()

		// Attempt to search for anime (spinner is inside api.SearchAnimeEnhanced)
		// Respect user's --source flag (e.g. --source anidb) via GlobalSource
		source := util.GlobalSource
		if source != "" {
			util.Debugf("Searching for: %s (source: %s)", currentName, source)
		} else {
			util.Debugf("Searching for: %s (searching all sources)", currentName)
		}
		anime, searchErr := searchWithRetryFn(currentName, source)

		if searchErr == nil && anime != nil {
			util.Debugf("[PERF] SearchAnimeWithRetry completed in %v", time.Since(searchStart))
			return anime, nil
		}

		// Anything but "back" is shown on the prompt screen's footer notice.
		if !errors.Is(searchErr, api.ErrBackToSearch) {
			util.Errorf("No anime found with the name: %s", currentName)
		}

		nextName, promptErr := promptForNameFn(currentName)
		if promptErr != nil {
			return nil, promptErr
		}
		currentName = nextName
	}
}

// FetchAnimeDetails enriches anime with metadata from AniList and/or the
// source provider. Spinner runs around the enrichment via runSpinnerFn.
func FetchAnimeDetails(anime *models.Anime) {
	detailsStart := time.Now()
	runSpinnerFn("Fetching anime details...", func() {
		fetchAnimeDetailsCore(anime)
	})
	util.Debugf("[PERF] FetchAnimeDetails completed in %v", time.Since(detailsStart))
}

// fetchAnimeDetailsCore is the pure orchestration: branch on source / metadata
// state, dispatch to aniListFetchFn / sourceDetailsFetchFn. Fully testable
// with mocks — no TUI, no time-sensitive code paths.
func fetchAnimeDetailsCore(anime *models.Anime) {
	if anime == nil {
		return
	}
	// Movie/TV catalogs skip AniList and use the source's own details.
	if anime.HasInteractiveEpisodeFlow() {
		util.Debugf("Skipping AniList enrichment for movie/TV content: %s (source: %s)", anime.Name, anime.Source)
		if err := sourceDetailsFetchFn(anime); err != nil {
			util.Debugf("Failed to enrich content: %v", err)
		}
		return
	}

	// AniList is the single enrichment path for anime.
	if anime.AnilistID <= 0 || anime.MalID <= 0 || anime.ImageURL == "" {
		enrichFromAniList(anime)
		return
	}
	util.Debugf("AniList data already present (ID: %d, MAL: %d), skipping redundant fetch", anime.AnilistID, anime.MalID)
}

// enrichFromAniList fetches AniList metadata via aniListFetchFn and applies
// it to the anime. Errors are logged at debug level and ignored — the call
// site treats AniList as best-effort.
func enrichFromAniList(anime *models.Anime) {
	aniListInfo, err := aniListFetchFn(anime.Name)
	if err != nil {
		util.Debugf("Failed to fetch from AniList: %v", err)
		return
	}
	anime.AnilistID = aniListInfo.Data.Media.ID
	anime.MalID = aniListInfo.Data.Media.IDMal
	anime.Details = aniListInfo.Data.Media
	if cover := aniListInfo.Data.Media.CoverImage.Large; cover != "" {
		anime.ImageURL = cover
	}
	util.Debugf("Anime enriched with AniList data - ID: %d, MAL: %d", anime.AnilistID, anime.MalID)
}

// GetAnimeEpisodes fetches the episode list for the given anime from its source.
// FlixHQ content bypasses the spinner since it has its own UI interactions.
func GetAnimeEpisodes(anime *models.Anime) ([]models.Episode, error) {
	episodesStart := time.Now()

	var episodes []models.Episode
	var fetchErr error

	// No spinner when the fetch can open its own UI (season-selection
	// fuzzyfinder): a spinner animating over the finder eats the prompt text
	// and corrupts terminal state. HasInteractiveEpisodeFlow matches SuperFlix
	// by source because its catalog tags western animation as anime.
	if anime.HasInteractiveEpisodeFlow() {
		episodes, fetchErr = getAnimeEpisodesEnhancedFn(anime)
	} else {
		runSpinnerFn("Loading episodes...", func() {
			episodes, fetchErr = getAnimeEpisodesEnhancedFn(anime)
		})
	}

	if fetchErr != nil {
		return nil, fmt.Errorf("failed to fetch episodes: %w", fetchErr)
	}
	if len(episodes) == 0 {
		return nil, fmt.Errorf("the selected anime does not have episodes on the server")
	}

	util.Debugf("[PERF] GetAnimeEpisodes completed in %v", time.Since(episodesStart))
	return episodes, nil
}

// GetAnimeEpisodesLegacy is the URL-based compatibility shim.
func GetAnimeEpisodesLegacy(url string) ([]models.Episode, error) {
	episodesStart := time.Now()

	var episodes []models.Episode
	var fetchErr error

	runSpinnerFn("Loading episodes...", func() {
		episodes, fetchErr = getAnimeEpisodesLegacyFn(url)
	})

	if fetchErr != nil {
		return nil, fmt.Errorf("failed to fetch episodes: %w", fetchErr)
	}
	if len(episodes) == 0 {
		return nil, fmt.Errorf("the selected anime does not have episodes on the server")
	}

	util.Debugf("[PERF] GetAnimeEpisodesLegacy completed in %v", time.Since(episodesStart))
	return episodes, nil
}
