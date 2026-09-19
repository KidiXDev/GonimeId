package providers

import (
	"context"
	"testing"

	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpisodeNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   *models.Episode
		want string
	}{
		{"nil episode", nil, ""},
		{"empty episode", &models.Episode{}, ""},
		{"Number string set", &models.Episode{Number: "12"}, "12"},
		{"Num int set", &models.Episode{Num: 7}, "7"},
		{"Number wins over Num", &models.Episode{Number: "abc", Num: 5}, "abc"},
		{"Num zero falls through", &models.Episode{Num: 0}, ""},
		{"Num negative ignored", &models.Episode{Num: -1}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EpisodeNumber(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIDSubProviders_DescribeAndScraper(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind     source.SourceKind
		st       scraper.ScraperType
		priority int
		host     string
	}{
		{source.Otakudesu, scraper.OtakudesuType, 10, "otakudesu"},
		{source.Samehadaku, scraper.SamehadakuType, 20, "samehadaku"},
		{source.Nimegami, scraper.NimegamiType, 30, "nimegami"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			s, ok := source.Registered(tt.kind)
			require.True(t, ok)
			p, ok := s.(*idSubProvider)
			require.True(t, ok, "live registration must be an idSubProvider")
			d := p.Describe()
			assert.Equal(t, tt.kind, d.Kind)
			assert.Equal(t, tt.priority, d.Priority)
			assert.Contains(t, d.Explicit, string(tt.kind))
			assert.Equal(t, []string{tt.host}, d.URLMatchers)
			assert.NotEmpty(t, d.ProbeURL)
			assert.False(t, p.HasSeasons())
			ad, err := p.scraper()
			require.NoError(t, err)
			assert.Equal(t, tt.st, ad.GetType())
			_, isCtx := ad.(scraper.ContextualScraper)
			assert.True(t, isCtx, "adapter must honour cancellation")
			_, isQL := ad.(scraper.QualityLister)
			assert.True(t, isQL, "adapter must offer the quality picker")
		})
	}
}

// TestSourceRegistry_LiveSourcesRegistered verifies init() populated the
// Model B registry with every live source.
func TestSourceRegistry_LiveSourcesRegistered(t *testing.T) {
	t.Parallel()
	for _, kind := range []source.SourceKind{source.Otakudesu, source.Samehadaku, source.Nimegami} {
		s, ok := source.Registered(kind)
		require.True(t, ok, "source %s must be registered", kind)
		assert.Equal(t, kind, s.Describe().Kind)
	}
}

// TestResolve_LiveRegistry resolves against the REAL registry populated by
// this package's init() — it pins the production descriptors' matching
// behavior end to end (the source-package tests use mirrored fakes).
func TestResolve_LiveRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		anime    *models.Anime
		wantKind source.SourceKind
	}{
		{"nil anime", nil, source.Unknown},
		{"empty anime", &models.Anime{}, source.Unknown},
		{"explicit wins over URL", &models.Anime{Source: "Samehadaku", URL: "https://otakudesu.blog/anime/x/"}, source.Samehadaku},
		{"explicit Samehadaku", &models.Anime{Source: "Samehadaku"}, source.Samehadaku},
		{"otakudesu tag", &models.Anime{Name: "Naruto [Otakudesu]"}, source.Otakudesu},
		{"PT-BR tag is no longer routed anywhere", &models.Anime{Name: "Naruto [PT-BR]"}, source.Unknown},
		{"explicit Otakudesu", &models.Anime{Source: "Otakudesu"}, source.Otakudesu},
		{"otakudesu URL", &models.Anime{URL: "https://otakudesu.blog/anime/naruto-sub-indo/"}, source.Otakudesu},
		{"samehadaku URL", &models.Anime{URL: "https://v2.samehadaku.how/anime/naruto-kecil/"}, source.Samehadaku},
		{"nimegami episode URL", &models.Anime{URL: "https://nimegami.id/sousou-no-frieren-sub-indo/#play_eps_1"}, source.Nimegami},
		{"unknown", &models.Anime{Name: "X", URL: "https://example.com/v"}, source.Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, resolved := source.Resolve(tt.anime)
			assert.Equal(t, tt.wantKind, resolved.Kind, "reason: %s", resolved.Reason)
			if tt.wantKind != source.Unknown {
				require.NotNil(t, src)
				assert.Equal(t, tt.wantKind, src.Describe().Kind)
			}
		})
	}
}

// TestResolveURL_LiveRegistry mirrors TestResolve_LiveRegistry for URL-only
// resolution against the real registered descriptors.
func TestResolveURL_LiveRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url      string
		wantKind source.SourceKind
	}{
		{"", source.Unknown},
		{"https://otakudesu.blog/episode/naruto-episode-1-sub-indo/", source.Otakudesu},
		{"https://v2.samehadaku.how/naruto-episode-1/", source.Samehadaku},
		{"https://nimegami.id/sousou-no-frieren-sub-indo/#play_eps_1", source.Nimegami},
		// Removed hosts resolve to nothing rather than to a guess.
		{"https://animefire.plus/ep/naruto-1", source.Unknown},
		{"https://example.com/video", source.Unknown},
	}
	for _, tt := range tests {
		t.Run("url="+tt.url, func(t *testing.T) {
			t.Parallel()
			_, resolved := source.ResolveURL(tt.url)
			assert.Equal(t, tt.wantKind, resolved.Kind, "reason: %s", resolved.Reason)
		})
	}
}

// fakeLister is a UnifiedScraper that also advertises QualityLister; Qualities
// must never be called when there is no terminal to show a picker on.
type fakeLister struct {
	scraper.UnifiedScraper
	called bool
}

func (f *fakeLister) Qualities(context.Context, string) ([]string, error) {
	f.called = true
	return []string{"720p", "480p"}, nil
}

// TestPickQuality_NoTerminalKeepsDefault pins the headless behaviour (CI, a
// pipe): an explicit quality passes through, and "best" stays "best" without
// touching the source, so nothing can block waiting on a picker.
func TestPickQuality_NoTerminalKeepsDefault(t *testing.T) {
	// Not parallel: relies on the process having no TTY on stdin, which is
	// true under `go test` but is process-wide state.
	for _, q := range []string{"", "best", "720p"} {
		f := &fakeLister{}
		got, err := pickQuality(context.Background(), f, "https://otakudesu.blog/episode/x/", q)
		require.NoError(t, err)
		assert.Equal(t, q, got)
		if q == "720p" {
			continue
		}
		assert.False(t, f.called, "no terminal → no picker → Qualities must not be fetched")
	}
}
