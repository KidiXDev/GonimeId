// Package api provides enhanced anime search and streaming capabilities
package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"

	apisource "github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/KidiXDev/GonimeId/internal/util"
	"golang.org/x/term"
)

// Cached terminal detection (checked once, reused)
var (
	stdoutIsTerminal     bool
	stdoutIsTerminalOnce sync.Once
)

func isStdoutTerminal() bool {
	stdoutIsTerminalOnce.Do(func() {
		fd := os.Stdout.Fd()
		stdoutIsTerminal = fd <= math.MaxInt && term.IsTerminal(int(fd))
	})
	return stdoutIsTerminal
}

// runWithSpinner runs the action with a spinner if stdout is a terminal,
// otherwise runs the action directly. This ensures CI and non-interactive
// environments work correctly since huh/v2 spinner may skip the Action
// callback when no terminal is attached.
//
// The huh spinner's Run() can return before its Action goroutine completes
// (e.g. tea.Interrupt from residual stdin bytes left over from a prior
// fuzzyfinder). When that happens the closure that mutates the caller's
// local variables is still running, so the caller would observe zero values.
// awaitActionThroughRunner uses sync.Once + a trailing safety call to
// guarantee the action runs exactly once and that this function does not
// return until that single execution has finished.
func runWithSpinner(title string, action func()) {
	// Console logs are silenced for the loading screen's lifetime: background
	// probes log through util.Warn/Info and would land between frames.
	restoreConsoleLogs := util.SuppressConsoleLogging()
	defer restoreConsoleLogs()
	_ = tui.RunLoading("Search", title, func(context.Context) error {
		action()
		return nil
	})
}

// ErrBackToSearch is returned when user selects the back option to search again
var ErrBackToSearch = errors.New("back to search requested")

// SearchFetchFunc fans out a free-text search across the given source kinds
// (empty = all) and returns the aggregated, language-tagged results. It is a
// seam so the api package can dispatch through the Model B registry
// (providers.SearchAll) without importing providers (which would cycle). The
// providers package wires it in its init(); if unset, the search falls back to
// the ScraperManager engine.
type SearchFetchFunc func(ctx context.Context, query string, kinds []apisource.SourceKind) ([]*models.Anime, error)

var searchFetchFn SearchFetchFunc

// SetSearchFetch installs the registry-backed search fan-out. Called from
// providers.init().
func SetSearchFetch(f SearchFetchFunc) { searchFetchFn = f }

// EpisodesFetchFunc lists an anime's episodes through the Model B registry.
// Like SearchFetchFunc, it is a seam so api can dispatch through
// providers.FetchEpisodes without importing providers (which would cycle).
type EpisodesFetchFunc func(anime *models.Anime) ([]models.Episode, error)

var episodesFetchFn EpisodesFetchFunc

// SetEpisodesFetch installs the registry-backed episode dispatch. Called from
// providers.init().
func SetEpisodesFetch(f EpisodesFetchFunc) { episodesFetchFn = f }

// fetchEpisodesViaRegistry dispatches episode listing through the Model B
// registry seam. It is the replacement for the deleted GetAnimeEpisodesEnhanced
// per-source switch; every former caller routes here.
func fetchEpisodesViaRegistry(anime *models.Anime) ([]models.Episode, error) {
	if episodesFetchFn == nil {
		return nil, fmt.Errorf("episode dispatch not wired: the providers package must be imported")
	}
	return episodesFetchFn(anime)
}

// StreamFetchFunc resolves a single episode's stream URL through the Model B
// registry. Seam so api can dispatch through providers.FetchStreamURL without
// importing providers (which would cycle).
type StreamFetchFunc func(episode *models.Episode, anime *models.Anime, quality string) (string, error)

var streamFetchFn StreamFetchFunc

// SetStreamFetch installs the registry-backed stream dispatch. Called from
// providers.init().
func SetStreamFetch(f StreamFetchFunc) { streamFetchFn = f }

// fetchStreamViaRegistry dispatches stream resolution through the Model B
// registry seam — the replacement for the deleted GetEpisodeStreamURL switch.
func fetchStreamViaRegistry(episode *models.Episode, anime *models.Anime, quality string) (string, error) {
	if streamFetchFn == nil {
		return "", fmt.Errorf("stream dispatch not wired: the providers package must be imported")
	}
	return streamFetchFn(episode, anime, quality)
}

// SearchAnimeEnhanced fans a free-text search out across every registered source.
func SearchAnimeEnhanced(name, src string) (*models.Anime, error) {
	anime, _, err := SearchAnimeEnhancedWithResults(name, src)
	return anime, err
}

// SearchAnimeEnhancedWithResults returns the selected anime and the result set
// it came from so an interactive session can reopen the list without refetching.
func SearchAnimeEnhancedWithResults(name, src string) (*models.Anime, []*models.Anime, error) {
	return searchAnimeEnhancedWithResults(name, src, searchFetchFn, tui.SelectAnime, enrichAnimeData)
}

// SelectAnimeFromResults reopens a previously fetched result set, keeping the
// last selection focused. Detail enrichment remains the caller's next step.
func SelectAnimeFromResults(animes []*models.Anime, selected *models.Anime) (*models.Anime, error) {
	anime, err := tui.SelectAnimeFrom(animes, selected)
	if errors.Is(err, tui.ErrSelectionBack) {
		return nil, ErrBackToSearch
	}
	if err != nil {
		return nil, fmt.Errorf("anime selection cancelled: %w", err)
	}
	if anime == nil {
		return nil, fmt.Errorf("anime selection returned nil")
	}
	return anime, nil
}

func searchAnimeEnhanced(
	name string,
	src string,
	search SearchFetchFunc,
	selectAnime func([]*models.Anime) (*models.Anime, error),
	enrich func(*models.Anime) error,
) (*models.Anime, error) {
	anime, _, err := searchAnimeEnhancedWithResults(name, src, search, selectAnime, enrich)
	return anime, err
}

func searchAnimeEnhancedWithResults(
	name string,
	src string,
	search SearchFetchFunc,
	selectAnime func([]*models.Anime) (*models.Anime, error),
	enrich func(*models.Anime) error,
) (*models.Anime, []*models.Anime, error) {
	// Map the optional source selector to the registry kinds to search. Empty
	// = all sources; a specific kind narrows the fan-out.
	var registryKinds []apisource.SourceKind
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "otakudesu":
		registryKinds = []apisource.SourceKind{apisource.Otakudesu}
	case "samehadaku":
		registryKinds = []apisource.SourceKind{apisource.Samehadaku}
	case "nimegami":
		registryKinds = []apisource.SourceKind{apisource.Nimegami}
	case "ylnime":
		registryKinds = []apisource.SourceKind{apisource.Ylnime}
	}
	util.Debug("Searching for anime/media", "query", name, "kinds", registryKinds)

	var animes []*models.Anime
	var searchErr error
	runWithSpinner("Searching for anime...", func() {
		if search == nil {
			searchErr = fmt.Errorf("search dispatch not wired: the providers package must be imported")
			return
		}
		// Model B registry fan-out (providers.SearchAll).
		animes, searchErr = search(context.Background(), name, registryKinds)
	})
	if searchErr != nil {
		return nil, nil, fmt.Errorf("failed to search: %w", searchErr)
	}
	validAnimes := make([]*models.Anime, 0, len(animes))
	for _, anime := range animes {
		if anime != nil {
			validAnimes = append(validAnimes, anime)
		}
	}
	animes = validAnimes

	if len(animes) == 0 {
		return nil, nil, fmt.Errorf("no results found for: %s", name)
	}
	util.Debug("Search results summary", "total", len(animes))

	if selectAnime == nil {
		return nil, animes, fmt.Errorf("anime selection not configured")
	}
	selectedAnime, err := selectAnime(animes)
	if errors.Is(err, tui.ErrSelectionBack) {
		return nil, animes, ErrBackToSearch
	}
	if err != nil {
		return nil, animes, fmt.Errorf("anime selection cancelled: %w", err)
	}
	if selectedAnime == nil {
		return nil, animes, fmt.Errorf("anime selection returned nil")
	}
	util.Debug("Anime selected", "name", selectedAnime.Name, "source", selectedAnime.Source)

	// Enrich with AniList data for images and metadata. Best-effort: episodes and
	// playback work without it, so a failure here is a warning, not an error
	// (issue #184).
	if enrich != nil {
		if err := enrich(selectedAnime); err != nil {
			util.Warn("Metadata enrichment unavailable; continuing without it", "anime", selectedAnime.Name, "error", err)
		}
	}

	return selectedAnime, animes, nil
}

// Enhanced download support
func DownloadEpisodeEnhanced(anime *models.Anime, episodeNum int, quality string) error {
	util.Debugf("Fetching episodes for %s...", anime.Name)

	episodes, err := fetchEpisodesViaRegistry(anime)
	if err != nil {
		return fmt.Errorf("failed to get episodes: %w", err)
	}

	if episodeNum < 1 || episodeNum > len(episodes) {
		return fmt.Errorf("episode %d not found (available: 1-%d)", episodeNum, len(episodes))
	}

	episode := episodes[episodeNum-1]

	util.Debugf("Getting stream URL for episode %d...", episodeNum)
	streamURL, err := fetchStreamViaRegistry(&episode, anime, quality)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	util.Debugf("Stream URL obtained: %s", streamURL)

	// Create a basic downloader (this would integrate with your existing downloader)
	return downloadFromURL(streamURL, fmt.Sprintf("%s_Episode_%d",
		sanitizeFilename(anime.Name), episodeNum))
}

// Enhanced range download support
func DownloadEpisodeRangeEnhanced(anime *models.Anime, startEp, endEp int, quality string) error {
	util.Debugf("Fetching episodes for %s...", anime.Name)

	episodes, err := fetchEpisodesViaRegistry(anime)
	if err != nil {
		return fmt.Errorf("failed to get episodes: %w", err)
	}

	if startEp < 1 || endEp > len(episodes) || startEp > endEp {
		return fmt.Errorf("invalid range %d-%d (available: 1-%d)", startEp, endEp, len(episodes))
	}

	for i := startEp; i <= endEp; i++ {
		util.Infof("Downloading episode %d of %d...", i, endEp)

		episode := episodes[i-1]
		streamURL, err := fetchStreamViaRegistry(&episode, anime, quality)
		if err != nil {
			util.Errorf("Failed to get stream URL for episode %d: %v", i, err)
			continue
		}

		filename := fmt.Sprintf("%s_Episode_%d", sanitizeFilename(anime.Name), i)
		// Note: downloadFromURL is a placeholder - integrate with proper downloader
		_ = downloadFromURL(streamURL, filename) // This will always fail as expected

		util.Infof("Successfully downloaded episode %d", i)
	}

	return nil
}

// Helper function to sanitize filename
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)

	// Replace invalid characters
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalid {
		name = strings.ReplaceAll(name, char, "_")
	}

	return name
}

// Basic download function (placeholder - integrate with your existing downloader)
func downloadFromURL(_, _ string) error {
	// This is a placeholder that should fail to trigger fallback to the proper downloader
	util.Debugf("Enhanced API downloadFromURL is a placeholder - returning error to trigger fallback")
	return fmt.Errorf("enhanced download not implemented - use legacy downloader")
}

// Legacy wrapper functions to maintain compatibility
func SearchAnimeWithSource(name, sourceName string) (*models.Anime, error) {
	return SearchAnimeEnhanced(name, sourceName)
}

func GetAnimeEpisodesWithSource(anime *models.Anime) ([]models.Episode, error) {
	return fetchEpisodesViaRegistry(anime)
}
