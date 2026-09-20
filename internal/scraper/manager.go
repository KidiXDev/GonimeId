// Package scraper provides per-source adapters over a unified interface.
//
// Sources self-register in internal/api/providers (Model B registry) and are
// fanned out by providers.SearchAll / source.Resolve. What lives here is the
// adapter layer — a thin wrapper that exposes each per-source client through
// the UnifiedScraper interface — plus NewAdapter, which the providers build
// lazily and own directly.
package scraper

import (
	"context"
	"fmt"

	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/astronime"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/moenime"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/nimegami"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/otakudesu"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/samehadaku"
	"github.com/KidiXDev/GonimeId/internal/scraper/providers/ylnime"
)

// ScraperType represents different scraper types
type ScraperType int

const (
	OtakudesuType  ScraperType = iota // otakudesu — Indonesian-subtitled
	SamehadakuType                    // samehadaku — Indonesian-subtitled
	NimegamiType                      // nimegami.id — Indonesian-subtitled
	YlnimeType                        // ylnime.com — Indonesian-subtitled
	MoenimeType                       // moenime.com / moeclip.com — Indonesian-subtitled
	AstronimeType                     // astronime.id / abyssplayer.com — Indonesian-subtitled
)

// ContextualScraper is the optional capability (Model C: discovered by type
// assertion, never by a flag) for adapters whose client honours cancellation
// end to end. UnifiedScraper predates context and cannot carry one.
type ContextualScraper interface {
	SearchAnimeContext(ctx context.Context, query string, options ...any) ([]*models.Anime, error)
	GetAnimeEpisodesContext(ctx context.Context, animeURL string) ([]models.Episode, error)
	GetStreamURLContext(ctx context.Context, episodeURL string, options ...any) (string, map[string]string, error)
}

// QualityLister is the optional capability (Model C, discovered by type
// assertion) of an adapter that can enumerate an episode's resolutions before
// resolving one, so the provider can offer a picker when no --quality was given.
type QualityLister interface {
	Qualities(ctx context.Context, episodeURL string) ([]string, error)
}

// UnifiedScraper provides a common interface for all scrapers
type UnifiedScraper interface {
	SearchAnime(query string, options ...any) ([]*models.Anime, error)
	GetAnimeEpisodes(animeURL string) ([]models.Episode, error)
	GetStreamURL(episodeURL string, options ...any) (string, map[string]string, error)
	GetType() ScraperType
}

// NewAdapter constructs a standalone UnifiedScraper adapter for the given type,
// wrapping a freshly-built per-source client. The clients are cheap, lazy
// structs so construction does no network I/O.
func NewAdapter(t ScraperType) (UnifiedScraper, error) {
	switch t {
	case OtakudesuType:
		return &ctxAdapter{client: otakudesu.NewOtakudesuClient(), typ: OtakudesuType}, nil
	case SamehadakuType:
		return &ctxAdapter{client: samehadaku.NewSamehadakuClient(), typ: SamehadakuType}, nil
	case NimegamiType:
		return &ctxAdapter{client: nimegami.NewNimegamiClient(), typ: NimegamiType}, nil
	case YlnimeType:
		return &ctxAdapter{client: ylnime.NewYlnimeClient(), typ: YlnimeType}, nil
	case MoenimeType:
		return &ctxAdapter{client: moenime.NewMoenimeClient(), typ: MoenimeType}, nil
	case AstronimeType:
		return &ctxAdapter{client: astronime.NewAstronimeClient(), typ: AstronimeType}, nil
	default:
		return nil, fmt.Errorf("no adapter for scraper type %v", t)
	}
}

// scraperDisplayName returns a stable display name for the scraper type. It is
// the canonical Source spelling used by result tagging and diagnostics.
func scraperDisplayName(scraperType ScraperType) string {
	switch scraperType {
	case OtakudesuType:
		return "Otakudesu"
	case SamehadakuType:
		return "Samehadaku"
	case NimegamiType:
		return "Nimegami"
	case YlnimeType:
		return "YLnime"
	case MoenimeType:
		return "Moenime"
	case AstronimeType:
		return "Astronime"
	default:
		return "Unknown"
	}
}

// ctxClient is the shape shared by the context-aware leaf clients; ctxAdapter
// exposes any of them as a ContextualScraper + QualityLister.
type ctxClient interface {
	SearchAnime(ctx context.Context, query string) ([]*models.Anime, error)
	GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error)
	GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (string, map[string]string, error)
	Qualities(ctx context.Context, episodeURL string) ([]string, error)
}

type ctxAdapter struct {
	client ctxClient
	typ    ScraperType
}

func (a *ctxAdapter) SearchAnimeContext(ctx context.Context, query string, _ ...any) ([]*models.Anime, error) {
	return a.client.SearchAnime(ctx, query)
}

func (a *ctxAdapter) GetAnimeEpisodesContext(ctx context.Context, animeURL string) ([]models.Episode, error) {
	return a.client.GetAnimeEpisodes(ctx, animeURL)
}

// GetStreamURLContext accepts an optional quality string ("best", "1080p", …)
// as the first variadic option.
func (a *ctxAdapter) GetStreamURLContext(ctx context.Context, episodeURL string, options ...any) (streamURL string, metadata map[string]string, err error) {
	return a.client.GetEpisodeStreamURL(ctx, episodeURL, qualityOption(options))
}

func (a *ctxAdapter) SearchAnime(query string, options ...any) ([]*models.Anime, error) {
	return a.SearchAnimeContext(context.Background(), query, options...)
}

func (a *ctxAdapter) GetAnimeEpisodes(animeURL string) ([]models.Episode, error) {
	return a.GetAnimeEpisodesContext(context.Background(), animeURL)
}

func (a *ctxAdapter) GetStreamURL(episodeURL string, options ...any) (streamURL string, metadata map[string]string, err error) {
	return a.GetStreamURLContext(context.Background(), episodeURL, options...)
}

func (a *ctxAdapter) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	return a.client.Qualities(ctx, episodeURL)
}

func (a *ctxAdapter) GetType() ScraperType { return a.typ }

// qualityOption reads the optional quality string from a variadic option list.
func qualityOption(options []any) string {
	if len(options) > 0 {
		if q, ok := options[0].(string); ok && q != "" {
			return q
		}
	}
	return "best"
}

// NewCtxAdapterForTest wraps a leaf client already pointed at a test server so
// registry-level tests can drive the real adapter glue offline. Only for tests.
func NewCtxAdapterForTest(client ctxClient, typ ScraperType) UnifiedScraper {
	return &ctxAdapter{client: client, typ: typ}
}
