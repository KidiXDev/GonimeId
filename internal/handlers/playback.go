package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/KidiXDev/GonimeId/internal/api"
	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/appflow"
	"github.com/KidiXDev/GonimeId/internal/discord"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/playback"
	"github.com/KidiXDev/GonimeId/internal/player"
	"github.com/KidiXDev/GonimeId/internal/tracking"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/KidiXDev/GonimeId/internal/util"
	"github.com/KidiXDev/GonimeId/internal/version"
	"github.com/charmbracelet/x/term"
)

var errSavedMediaUnavailable = errors.New("saved media is unavailable")

// HandlePlaybackMode processes normal anime playback
func HandlePlaybackMode(animeName string) {
	timer := util.StartTimer("PlaybackMode:Total")
	defer timer.Stop()

	// Root context for the playback session. Today it is Background; once the
	// dispatch path honors ctx end to end, this becomes the single place to
	// hook signal-aware cancellation (signal.NotifyContext).
	ctx := context.Background()

	// Initialize the beautiful logger
	util.InitLogger()

	// Interactive session: nothing prints to the console between screens.
	// Every line still reaches the log file and the in-app log overlay
	// (ctrl+l), and the newest WARN/ERROR shows in each screen's footer.
	// Restored on return so a final error is printed after the TUI closes.
	if term.IsTerminal(os.Stdin.Fd()) {
		restore := util.SuppressConsoleLogging()
		defer func() {
			tui.CloseScreens()
			restore()
			util.EchoLastError()
		}()
	}

	// Confirm the manual kill-switch (S1) visibly: if the user disabled any
	// source via GONIMEID_DISABLED_SOURCES, say so once at startup so a turned-
	// off source is never a silent surprise (R5).
	if disabled := source.DisabledSources(); len(disabled) > 0 {
		names := make([]string, len(disabled))
		for i, k := range disabled {
			names[i] = string(k)
		}
		util.Warnf("Sources disabled by config (GONIMEID_DISABLED_SOURCES): %s", strings.Join(names, ", "))
	}

	// Pre-warm connections are now started in main() so they run while the
	// user is still typing the anime name. This call is a noop (sync.Once).
	util.PreWarmConnections()

	tracking.HandleTrackingNotice()
	util.Debugf("[PERF] starting GonimeId v%s", version.Version)

	// Discord init runs in background - doesn't block startup
	discordManager := discord.NewManager()
	_ = discordManager.Initialize() // Non-blocking, runs async
	defer discordManager.Shutdown()

	currentAnimeName := animeName
	searchSession := &appflow.SearchSession{}
	hubMode := animeName == ""

	for {
		var anime *models.Anime
		fromHistory := false
		var err error

		if hubMode && currentAnimeName == "" {
			var search, quit bool
			anime, search, quit = watchHubSelection()
			if quit {
				return
			}
			if !search && anime == nil {
				continue
			}
			fromHistory = anime != nil
		}

		if anime == nil {
			searchTimer := util.StartTimer("SearchAnime:WithRetry")
			anime, err = searchSession.SearchWithRetry(currentAnimeName)
			searchTimer.Stop()
		}

		if err != nil {
			if hubMode && tui.IsCancelled(err) {
				currentAnimeName = ""
				continue
			}
			if !tui.IsCancelled(err) {
				util.Errorf("Failed to search for anime: %v", err)
			}
			return
		}

		playbackErr := playSelectedMedia(ctx, anime, discordManager.IsEnabled(), fromHistory)
		if hubMode {
			if fromHistory && errors.Is(playbackErr, errSavedMediaUnavailable) {
				util.Warn("Saved source is unavailable; searching by title", "title", anime.Name)
				currentAnimeName = util.TreatingAnimeName(anime.Name)
				continue
			}
			currentAnimeName = ""
			searchSession.Reset()
			continue
		}

		if errors.Is(playbackErr, player.ErrBackToAnimeSelection) {
			util.Infof("Going back to anime selection...")
			continue
		}
		break
	}
}

func playSelectedMedia(ctx context.Context, anime *models.Anime, discordEnabled, fromHistory bool) error {
	var episodes []models.Episode
	var epErr error
	fetchTimer := util.StartTimer("FetchDetails+Episodes:Sequential")
	detailsTimer := util.StartTimer("FetchAnimeDetails")
	appflow.FetchAnimeDetails(anime)
	detailsTimer.Stop()
	episodesTimer := util.StartTimer("GetAnimeEpisodes")
	episodes, epErr = appflow.GetAnimeEpisodes(anime)
	if epErr != nil && !errors.Is(epErr, api.ErrBackToSearch) {
		util.Errorf("Failed to get episodes: %v", epErr)
	}
	episodesTimer.Stop()
	fetchTimer.Stop()
	if errors.Is(epErr, api.ErrBackToSearch) {
		return player.ErrBackToAnimeSelection
	}
	if epErr != nil {
		return fmt.Errorf("%w: %v", errSavedMediaUnavailable, epErr)
	}
	if len(episodes) == 0 {
		return fmt.Errorf("%w: no episodes found", errSavedMediaUnavailable)
	}
	util.PerfCount("anime_loaded")
	totalEpisodes := len(episodes)
	playbackTimer := util.StartTimer("Playback:Handle")
	defer playbackTimer.Stop()
	if useMovieEpisodeSelector(anime, fromHistory) {
		return playback.HandleMovieWithEpisodeSelection(ctx, anime, episodes, discordEnabled)
	}
	if useEpisodeSelector(anime, totalEpisodes, fromHistory) {
		return playback.HandleSeries(ctx, anime, episodes, totalEpisodes, discordEnabled)
	}
	return playback.HandleMovie(ctx, anime, episodes, discordEnabled)
}

func useEpisodeSelector(anime *models.Anime, totalEpisodes int, fromHistory bool) bool {
	return !anime.IsMovie() && (fromHistory || totalEpisodes > 1)
}

func useMovieEpisodeSelector(anime *models.Anime, fromHistory bool) bool {
	return fromHistory && anime.IsMovie()
}

func watchHubSelection() (anime *models.Anime, search, quit bool) {
	tracker := player.GetTracker()
	for {
		var series []tracking.Series
		autoplay := true
		if tracker != nil {
			entries, err := tracker.GetAllAnime()
			if err != nil {
				util.Warnf("Could not load watch history: %v", err)
			} else {
				series = tracking.GroupSeries(entries)
			}
			autoplay = tracker.Autoplay()
		}

		result, err := tui.RunHome(series, autoplay, tracker != nil)
		if err != nil {
			if !tui.IsCancelled(err) {
				util.Warnf("Watch hub closed: %v", err)
			}
			return nil, false, true
		}
		switch result.Action {
		case tui.HomeSearch:
			return nil, true, false
		case tui.HomeOpenSeries:
			return mediaFromHistory(result.Series), false, false
		case tui.HomeToggleAutoplay:
			if tracker != nil {
				if err := tracker.SetAutoplay(!autoplay); err != nil {
					util.Warnf("Could not save autoplay preference: %v", err)
				}
			}
		case tui.HomeDeleteSeries:
			if tracker != nil {
				confirmed, _ := tui.Confirm("Remove " + tui.SingleLine(result.Series.Title) + " from history?")
				if confirmed {
					_ = tracker.DeleteSeries(result.Series.Key)
				}
			}
		case tui.HomeClearHistory:
			if tracker != nil {
				confirmed, _ := tui.Confirm("Clear all watch history?")
				if confirmed {
					_ = tracker.ClearHistory()
				}
			}
		default:
			return nil, false, true
		}
	}
}

func mediaFromHistory(series tracking.Series) *models.Anime {
	return &models.Anime{
		Name: series.Title, URL: series.URL, Source: series.Source,
		MediaType: models.MediaType(series.MediaType), AnilistID: series.AnilistID,
	}
}
