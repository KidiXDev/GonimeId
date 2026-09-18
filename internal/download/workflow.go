// Package download provides high-level download workflow management
package download

import (
	"context"
	"errors"
	"fmt"

	"github.com/KidiXDev/GonimeId/internal/api/providers"
	"github.com/KidiXDev/GonimeId/internal/api/providers/metadata"
	"github.com/KidiXDev/GonimeId/internal/appflow"
	"github.com/KidiXDev/GonimeId/internal/downloader"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/player"
	"github.com/KidiXDev/GonimeId/internal/util"
)

// workflowSearchFn is the anime search function used by HandleDownloadRequest.
// Tests may override it to avoid spawning a real TUI search.
var workflowSearchFn = appflow.SearchAnimeWithRetry

// workflowEnrichFn resolves the AniList season mapping for the found anime.
// Tests may override it to avoid the real AniList request.
var workflowEnrichFn = func(ctx context.Context, anime *models.Anime) ([]metadata.SeasonMapping, error) {
	return metadata.NewEnricher().EnrichAnime(ctx, anime)
}

// HandleDownloadRequest processes a download request from command line
func HandleDownloadRequest(request *util.DownloadRequest) error {
	util.Info("Starting enhanced download mode...")

	source := request.Source
	quality := request.Quality
	if quality == "" {
		quality = "best"
	}

	util.Infof("Using source: %s, quality: %s", source, quality)

	anime, err := workflowSearchFn(request.AnimeName)
	if err != nil {
		util.Errorf("Failed to search for anime: %v", err)
		return err
	}

	season := 1
	if request.SeasonNum > 0 {
		season = request.SeasonNum
	}
	player.SetAnimeName(anime.Name, season)
	player.SetExactMediaType(string(anime.MediaType))

	player.SetMediaMeta(&util.MediaMeta{
		OfficialTitle: anime.OfficialTitle(),
		Year:          anime.Year,
		TMDBID:        anime.TMDBID,
		IMDBID:        anime.IMDBID,
		AnilistID:     anime.AnilistID,
		MalID:         anime.MalID,
	})

	seasonMap, _ := workflowEnrichFn(context.Background(), anime)
	player.SetSeasonMap(seasonMap)

	player.SetMediaMeta(&util.MediaMeta{
		OfficialTitle: anime.OfficialTitle(),
		Year:          anime.Year,
		TMDBID:        anime.TMDBID,
		IMDBID:        anime.IMDBID,
		AnilistID:     anime.AnilistID,
		MalID:         anime.MalID,
	})

	if request.IsAll {
		util.Infof("Downloading ALL episodes of %s", anime.Name)
		eps, err := providers.FetchEpisodes(context.Background(), anime)
		if err == nil && len(eps) > 0 {
			dlErr := player.HandleBatchDownload(eps, anime)
			if dlErr == nil || errors.Is(dlErr, player.ErrUserQuit) {
				return nil
			}
			util.Infof("Batch download path failed, falling back to legacy: %v", dlErr)
		} else if err != nil {
			util.Infof("Enhanced episodes fetch failed: %v", err)
		}

		episodes, legacyErr := appflow.GetAnimeEpisodesLegacy(anime.URL)
		if legacyErr != nil {
			return fmt.Errorf("failed to fetch episodes: %w", legacyErr)
		}
		dl := downloader.NewEpisodeDownloaderWithAnime(episodes, anime.URL, anime)
		return dl.DownloadAllEpisodes()
	}

	if request.IsRange {
		util.Infof("Downloading episodes %d-%d of %s",
			request.StartEpisode, request.EndEpisode, anime.Name)

		eps, err := providers.FetchEpisodes(context.Background(), anime)
		if err == nil && len(eps) > 0 {
			dlErr := player.HandleBatchDownloadRange(eps, anime, request.StartEpisode, request.EndEpisode)
			if dlErr == nil || errors.Is(dlErr, player.ErrUserQuit) {
				return nil
			}
			util.Infof("Batch download path failed, falling back to legacy: %v", dlErr)
		} else if err != nil {
			util.Infof("Enhanced episodes fetch failed: %v", err)
		}
		episodes, legacyErr := appflow.GetAnimeEpisodesLegacy(anime.URL)
		if legacyErr != nil {
			return fmt.Errorf("failed to fetch episodes: %w", legacyErr)
		}
		dl := downloader.NewEpisodeDownloaderWithAnime(episodes, anime.URL, anime)
		return dl.DownloadEpisodeRange(request.StartEpisode, request.EndEpisode)
	}

	util.Infof("Downloading episode %d of %s", request.EpisodeNum, anime.Name)
	episodes, legacyErr := appflow.GetAnimeEpisodesLegacy(anime.URL)
	if legacyErr != nil {
		return fmt.Errorf("failed to fetch episodes: %w", legacyErr)
	}
	dl := downloader.NewEpisodeDownloaderWithAnime(episodes, anime.URL, anime)
	return dl.DownloadSingleEpisode(request.EpisodeNum)
}
