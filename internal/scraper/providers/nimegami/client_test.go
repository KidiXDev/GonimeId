package nimegami

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHostIsPinned fails loudly if the base host rotates without a deliberate
// edit (dated 2026-09-18).
func TestHostIsPinned(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://nimegami.id", nimegamiBase, "host rotated? update the const and this assertion together")
}

// Fixtures mirror the live markup captured 2026-09-18. Embed and file URLs
// point back at the test server so the probe never leaves the process.
const searchHTML = `<article><div class="thumbnail"><a href="BASE/sousou-no-frieren-season-2-sub-indo/"><img src="/s2.jpg"/></a></div>
<h2 itemprop='name'><a href="BASE/sousou-no-frieren-season-2-sub-indo/" title="Sousou no Frieren Season 2 Sub Indo : Episode 1 &#8211; 10 (End)">Sousou no Frieren Season 2</a></h2></article>
<article><h2 itemprop='name'><a href="BASE/sousou-no-frieren-sub-indo/" title="x">Sousou no Frieren Sub Indo</a></h2></article>
<article><h2 itemprop='name'><a href="" title="x">Broken card</a></h2></article>`

// payload encodes one episode's resolutions the way the site does.
func payload(base string, formats ...string) string {
	var entries []streamEntry
	for _, f := range formats {
		entries = append(entries, streamEntry{Format: f, URLs: []string{base + "/streaming/?f=" + f + "&ep=x", base + "/streaming/?f=" + f + "&ep=alt"}})
	}
	b, _ := json.Marshal(entries)
	return base64.StdEncoding.EncodeToString(b)
}

func newServer(t *testing.T) (*httptest.Server, *NimegamiClient) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") != "" {
			_, _ = w.Write([]byte(strings.ReplaceAll(searchHTML, "BASE", srv.URL)))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/sousou-no-frieren-sub-indo/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<h1 class="title">Sousou no Frieren Sub Indo : Episode 1 – 28 (End)</h1>
<div class="bg-hitam select-eps" id="play_eps_1"><h3>Play Episode 1</h3></div>
<div class="list_eps_stream">
<li class="select-eps" data="` + payload(srv.URL, "360p", "480p", "720p", "1080p") + `" id="play_eps_2" title="Sousou no Frieren Episode 2 Sub Indo"></li>
<li class="select-eps" data="` + payload(srv.URL, "480p", "720p") + `" id="play_eps_1" title="Sousou no Frieren Episode 1 Sub Indo"></li>
<li class="select-eps" data="not-base64!" id="play_eps_3"></li>
</div>`))
	})
	// berkasdrive stand-in: the 1080p file is gone (404); the "alt" server of
	// every format serves; the primary server of 720p serves too.
	mux.HandleFunc("/streaming/", func(w http.ResponseWriter, r *http.Request) {
		f, ep := r.URL.Query().Get("f"), r.URL.Query().Get("ep")
		_, _ = w.Write([]byte(`<video><source src="` + srv.URL + `/public/86/abc-` + f + `-` + ep + `.mp4?filename=[Nimegami] Sousou no Frieren Ep 01 (` + f + `).mp4" type="video/mp4"></video>`))
	})
	mux.HandleFunc("/public/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"), "probe must ask for one byte only")
		require.Empty(t, r.URL.RawQuery, "the unescaped ?filename= query must be dropped")
		switch {
		case strings.Contains(r.URL.Path, "1080p"):
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(r.URL.Path, "480p-x"):
			http.Redirect(w, r, srv.URL+"/cdn"+r.URL.Path, http.StatusFound) // the real CDN hops once
		default:
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
		}
	})
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, NewClientForTest(srv.URL)
}

func TestSearchAnime(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.SearchAnime(context.Background(), "sousou-no-frieren")
	require.NoError(t, err)
	require.Len(t, got, 2, "a card without a link is dropped")
	assert.Equal(t, "Sousou no Frieren Season 2", got[0].Name)
	assert.Equal(t, srv.URL+"/sousou-no-frieren-season-2-sub-indo/", got[0].URL)
	assert.Equal(t, "/s2.jpg", got[0].ImageURL)
	assert.Equal(t, "Sousou no Frieren", got[1].Name, "Sub Indo suffix stripped")
	assert.Equal(t, sourceLabel, got[1].Source)
}

func TestSearchAnime_EmptyQuery(t *testing.T) {
	t.Parallel()
	_, c := newServer(t)
	_, err := c.SearchAnime(context.Background(), " - ")
	require.Error(t, err)
}

func TestGetAnimeEpisodes(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	eps, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/sousou-no-frieren-sub-indo/")
	require.NoError(t, err)
	require.Len(t, eps, 2, "the header play button and the undecodable row are not episodes")
	assert.Equal(t, 1, eps[0].Num, "ascending regardless of page order")
	assert.Equal(t, srv.URL+"/sousou-no-frieren-sub-indo/#play_eps_1", eps[0].URL)
	assert.Equal(t, "Sousou no Frieren Episode 2", eps[1].Title.English, "Sub Indo suffix stripped so the picker can collapse it to \"Episode 2\"")
}

func TestGetAnimeEpisodes_NoList(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<p>nothing</p>`)) }))
	t.Cleanup(srv.Close)
	_, err := NewClientForTest(srv.URL).GetAnimeEpisodes(context.Background(), srv.URL+"/x/")
	require.Error(t, err)
}

func TestQualities(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.Qualities(context.Background(), srv.URL+"/sousou-no-frieren-sub-indo/#play_eps_2")
	require.NoError(t, err)
	assert.Equal(t, []string{"1080p", "720p", "480p", "360p"}, got)
}

func TestGetEpisodeStreamURL(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	ep2 := srv.URL + "/sousou-no-frieren-sub-indo/#play_eps_2"
	tests := []struct {
		name, quality, want string
	}{
		{"best skips the removed 1080p file and takes 720p", "best", srv.URL + "/public/86/abc-720p-x.mp4"},
		{"explicit 1080p is gone on both servers, falls back to 720p", "1080p", srv.URL + "/public/86/abc-720p-x.mp4"},
		{"480p follows the CDN redirect", "480p", srv.URL + "/public/86/abc-480p-x.mp4"},
		{"360p", "360p", srv.URL + "/public/86/abc-360p-x.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, meta, err := c.GetEpisodeStreamURL(context.Background(), ep2, tt.quality)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "nimegami", meta["source"])
		})
	}
}

func TestGetEpisodeStreamURL_BadURL(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	_, _, err := c.GetEpisodeStreamURL(context.Background(), srv.URL+"/sousou-no-frieren-sub-indo/", "best")
	require.Error(t, err, "a season page without an episode fragment is not an episode")
	_, _, err = c.GetEpisodeStreamURL(context.Background(), srv.URL+"/sousou-no-frieren-sub-indo/#play_eps_9", "best")
	require.Error(t, err, "an episode that is not on the page")
}

func TestGetEpisodeStreamURL_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(srv.Close)
	_, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/x/#play_eps_1", "best")
	require.Error(t, err)
}

// The season page is fetched once for episodes + qualities + stream.
func TestSeasonPage_CachedAcrossCalls(t *testing.T) {
	t.Parallel()
	var hits int
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/x/", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`<div class="list_eps_stream"><li class="select-eps" data="` + payload(srv.URL, "720p") + `" id="play_eps_1"></li></div>`))
	})
	mux.HandleFunc("/streaming/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<source src="` + srv.URL + `/f.mp4">`))
	})
	mux.HandleFunc("/f.mp4", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusPartialContent) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClientForTest(srv.URL)
	_, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/x/")
	require.NoError(t, err)
	_, err = c.Qualities(context.Background(), srv.URL+"/x/#play_eps_1")
	require.NoError(t, err)
	_, _, err = c.GetEpisodeStreamURL(context.Background(), srv.URL+"/x/#play_eps_1", "best")
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Sousou no Frieren", cleanTitle("Sousou no Frieren Sub Indo : Episode 1 – 28 (End)"))
	assert.Equal(t, "Sousou no Frieren", cleanTitle("Sousou no Frieren Sub Indo"))
	assert.Equal(t, "Naruto", cleanTitle("Naruto"))
	page, num := splitEpisodeURL("https://nimegami.id/x/#play_eps_12")
	assert.Equal(t, "https://nimegami.id/x/", page)
	assert.Equal(t, 12, num)
	_, num = splitEpisodeURL("https://nimegami.id/x/")
	assert.Equal(t, 0, num)
	assert.Equal(t, 1080, heightOf("1080p"))
	assert.Equal(t, 0, heightOf("best"))
}
