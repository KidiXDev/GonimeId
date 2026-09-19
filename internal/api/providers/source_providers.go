package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/scraper"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/KidiXDev/GonimeId/internal/util"
	"golang.org/x/term"
)

// lazyGetAdapter returns a standalone adapter for a scraper type, built once and
// cached. This is how each Model B provider owns its scraper directly.
type adapterSlot struct {
	scraper.UnifiedScraper
}

func lazyGetAdapter(once *sync.Once, cache *adapterSlot, st scraper.ScraperType) (scraper.UnifiedScraper, error) {
	once.Do(func() { cache.UnifiedScraper, _ = scraper.NewAdapter(st) })
	if cache.UnifiedScraper == nil {
		return nil, fmt.Errorf("no adapter for scraper type %v", st)
	}
	return cache.UnifiedScraper, nil
}

// EpisodeNumber extracts the episode number string from an Episode model.
// Returns "" if indeterminate — caller must decide how to handle.
func EpisodeNumber(ep *models.Episode) string {
	if ep == nil {
		return ""
	}
	if ep.Number != "" {
		return ep.Number
	}
	if ep.Num > 0 {
		return fmt.Sprintf("%d", ep.Num)
	}
	return ""
}

// --- Indonesian-subtitled providers ---
//
// These leaf clients are context-aware and share one adapter shape, so a single
// provider type parameterised by descriptor serves them. Otakudesu goes first:
// its download section reaches 1080p.

type idSubProvider struct {
	once    sync.Once
	adapter adapterSlot
	desc    source.Descriptor
	st      scraper.ScraperType
}

func init() {
	source.Register(&idSubProvider{
		st: scraper.OtakudesuType,
		desc: source.Descriptor{
			Kind:        source.Otakudesu,
			Priority:    10,
			Explicit:    []string{"Otakudesu"},
			Tags:        []string{"[otakudesu]"},
			URLMatchers: []string{"otakudesu"},
			ProbeURL:    "https://otakudesu.blog",
		},
	})
	source.Register(&idSubProvider{
		st: scraper.SamehadakuType,
		desc: source.Descriptor{
			Kind:        source.Samehadaku,
			Priority:    20,
			Explicit:    []string{"Samehadaku"},
			Tags:        []string{"[samehadaku]"},
			URLMatchers: []string{"samehadaku"},
			ProbeURL:    "https://v2.samehadaku.how",
		},
	})
	source.Register(&idSubProvider{
		st: scraper.NimegamiType,
		desc: source.Descriptor{
			Kind:        source.Nimegami,
			Priority:    30,
			Explicit:    []string{"Nimegami"},
			Tags:        []string{"[nimegami]"},
			URLMatchers: []string{"nimegami"},
			ProbeURL:    "https://nimegami.id",
		},
	})
	source.Register(&idSubProvider{
		st: scraper.YlnimeType,
		desc: source.Descriptor{
			Kind:        source.Ylnime,
			Priority:    40,
			Explicit:    []string{"YLnime"},
			Tags:        []string{"[ylnime]"},
			URLMatchers: []string{"ylnime.com"},
			ProbeURL:    "https://ylnime.com",
		},
	})
}

func (p *idSubProvider) scraper() (scraper.UnifiedScraper, error) {
	return lazyGetAdapter(&p.once, &p.adapter, p.st)
}

func (p *idSubProvider) Describe() source.Descriptor { return p.desc }

func (p *idSubProvider) HasSeasons() bool { return false }

// The adapter implements scraper.ContextualScraper, so every call below hands
// it the real context instead of letting a cancelled search keep an HTTP
// request alive until the client timeout. The type assertion is the Model C
// discovery pattern; the fallbacks keep this working if the capability is ever
// dropped.
func (p *idSubProvider) Search(ctx context.Context, query string) ([]*models.Anime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	adapter, err := p.scraper()
	if err != nil {
		return nil, err
	}
	var results []*models.Anime
	if ca, ok := adapter.(scraper.ContextualScraper); ok {
		results, err = ca.SearchAnimeContext(ctx, query)
	} else {
		results, err = adapter.SearchAnime(query)
	}
	if err != nil {
		return nil, err
	}
	tagResults(results, p.desc.Kind)
	return results, nil
}

func (p *idSubProvider) FetchEpisodes(ctx context.Context, anime *models.Anime) ([]models.Episode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	adapter, err := p.scraper()
	if err != nil {
		return nil, err
	}
	if ca, ok := adapter.(scraper.ContextualScraper); ok {
		return ca.GetAnimeEpisodesContext(ctx, anime.URL)
	}
	return adapter.GetAnimeEpisodes(anime.URL)
}

// FetchStreamURL must open with util.ClearGlobalSubtitles() and
// util.SetGlobalAnimeSource — skip them and the previous episode's subtitles
// leak into this one.
func (p *idSubProvider) FetchStreamURL(ctx context.Context, episode *models.Episode, anime *models.Anime, quality string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	util.ClearGlobalSubtitles()
	if anime.Source != "" {
		util.SetGlobalAnimeSource(anime.Source)
	}
	adapter, err := p.scraper()
	if err != nil {
		return "", err
	}
	quality, err = pickQuality(ctx, adapter, episode.URL, quality)
	if err != nil {
		return "", err
	}
	var url string
	if ca, ok := adapter.(scraper.ContextualScraper); ok {
		url, _, err = ca.GetStreamURLContext(ctx, episode.URL, quality)
	} else {
		url, _, err = adapter.GetStreamURL(episode.URL, quality)
	}
	if err != nil {
		return "", fmt.Errorf("%s stream: %w", strings.ToLower(string(p.desc.Kind)), err)
	}
	if url == "" {
		return "", fmt.Errorf("empty stream URL returned from %s", p.desc.Kind)
	}
	return url, nil
}

// PreselectQuality always runs the resolution picker for interactive playback,
// even when --quality or a previous source set util.GlobalQuality. This keeps a
// quality choice source-specific instead of silently reusing another source's.
func PreselectQuality(ctx context.Context, anime *models.Anime, episode *models.Episode, _ string) (string, error) {
	src, _ := source.Resolve(anime)
	p, ok := src.(*idSubProvider)
	if !ok {
		return "", fmt.Errorf("quality selection unavailable for this source")
	}
	if episode == nil {
		return "", fmt.Errorf("quality selection requires an episode")
	}
	adapter, err := p.scraper()
	if err != nil {
		return "", err
	}
	ql, ok := adapter.(scraper.QualityLister)
	if !ok {
		return "", fmt.Errorf("quality selection unavailable for %s", p.desc.Kind)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("quality selection requires an interactive terminal")
	}
	var qualities []string
	err = tui.RunLoading("Episodes › Quality", "Loading qualities…", func(lctx context.Context) error {
		var qerr error
		qualities, qerr = ql.Qualities(lctx, episode.URL)
		return qerr
	})
	switch {
	case tui.IsCancelled(err):
		return "", err
	case err != nil:
		return "", fmt.Errorf("list %s qualities: %w", p.desc.Kind, err)
	case len(qualities) == 0:
		return "", fmt.Errorf("no resolutions available from %s", p.desc.Kind)
	}
	return promptQuality(qualities)
}

// promptQuality shows the resolution picker and remembers the pick.
func promptQuality(qualities []string) (string, error) {
	idx, err := tui.PickLabels(qualities, tui.PickOptions{
		Breadcrumb:   "Episodes › Quality",
		WindowTitle:  "GonimeId - Quality",
		ItemSingular: "quality",
		ItemPlural:   "qualities",
	})
	if err != nil {
		return "", err
	}
	util.GlobalQuality = qualities[idx]
	util.Debug("Quality picked", "quality", qualities[idx], "offered", qualities)
	return qualities[idx], nil
}

// pickQuality is the fallback for non-playback callers that did not run
// PreselectQuality. Explicit and headless download choices remain automatic.
func pickQuality(ctx context.Context, adapter scraper.UnifiedScraper, episodeURL, quality string) (string, error) {
	if quality != "" && quality != "best" {
		return quality, nil
	}
	ql, ok := adapter.(scraper.QualityLister)
	if !ok || !term.IsTerminal(int(os.Stdin.Fd())) {
		return quality, nil
	}
	qualities, err := ql.Qualities(ctx, episodeURL)
	if err != nil {
		// Listing is best-effort: the resolver will report the real error.
		util.Debug("Quality listing failed; playing the source default", "error", err)
		return quality, nil
	}
	if len(qualities) < 2 {
		return quality, nil // nothing to choose
	}
	return promptQuality(qualities)
}
