package samehadaku

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHostIsPinned fails loudly if the base host rotates without a deliberate
// edit (dated 2026-09-18: samehadaku.how → v2.samehadaku.how).
func TestHostIsPinned(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://v2.samehadaku.how", samehadakuBase, "host rotated? update the const and this assertion together")
}

// Fixtures mirror the live markup captured 2026-09-18.
const (
	searchHTML = `<article class="animpost"><div class="animepost"><div class="animposx">
<a rel="37451" href="BASE/anime/naruto-kecil/" title="Naruto Kecil"><img src="/p.jpg"/><div class="data"><div class="title"><h2>Naruto Kecil</h2></div></div></a>
</div></div></article>
<article class="animpost"><div class="animposx"><a href="BASE/genre/action/" title="Action"></a></div></article>`
	animeHTML = `<div class="lstepsiode listeps"><ul>
<li><div class="epsright"><span class="eps"><a href="BASE/naruto-kecil-episode-220/">220</a></span></div><div class="epsleft"><span class="lchx"><a href="BASE/naruto-kecil-episode-220/">Naruto Kecil Episode 220</a></span></div></li>
<li><div class="epsright"><span class="eps"><a href="BASE/naruto-kecil-episode-1/">1</a></span></div><div class="epsleft"><span class="lchx"><a href="BASE/naruto-kecil-episode-1/">Naruto Kecil Episode 1</a></span></div></li>
</ul></div>`
	episodeHTML = `<div id="server"><ul>
<li><div id="player-option-1" class="east_player_option" data-post="37909" data-nume="1" data-type="schtml"><span>Blogspot</span></div></li>
<li><div id="player-option-5" class="east_player_option" data-post="37909" data-nume="5" data-type="schtml"><span>Vidhide 720p</span></div></li>
<li><div id="player-option-10" class="east_player_option" data-post="37909" data-nume="10" data-type="schtml"><span>Pixel 480p</span></div></li>
<li><div id="player-option-11" class="east_player_option" data-post="37909" data-nume="11" data-type="schtml"><span>Pixel 720p</span></div></li>
<li><div id="player-option-12" class="east_player_option" data-post="37909" data-nume="12" data-type="schtml"><span>Pixel 1080p</span></div></li>
</ul></div>`
)

func newServer(t *testing.T) (*httptest.Server, *SamehadakuClient) {
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
	mux.HandleFunc("/anime/naruto-kecil/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(animeHTML, "BASE", srv.URL)))
	})
	mux.HandleFunc("/naruto-kecil-episode-220/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(episodeHTML)) })
	// Pixeldrain stand-in: "Gone" is a removed file, everything else serves.
	mux.HandleFunc("/api/file/Gone", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/api/file/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"), "probe must ask for one byte only")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	})
	mux.HandleFunc("/wp-admin/admin-ajax.php", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "player_ajax", r.Form.Get("action"))
		require.Equal(t, "37909", r.Form.Get("post"))
		var src string
		switch r.Form.Get("nume") {
		case "1":
			src = "https://www.blogger.com/video.g?token=AD6v5dzYzfD2"
		case "10":
			src = "https://pixeldrain.com/u/Low480"
		case "11":
			src = "https://pixeldrain.com/u/UidvB5qB"
		case "12":
			src = "https://pixeldrain.com/u/Gone" // listed, but the file was removed
		default:
			src = "https://krakenfiles.com/embed-video/x"
		}
		_, _ = w.Write([]byte(`<iframe src="` + src + `" FRAMEBORDER=0></iframe>`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, NewClientForTest(srv.URL)
}

func TestSearchAnime(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.SearchAnime(context.Background(), "naruto")
	require.NoError(t, err)
	require.Len(t, got, 1, "the genre link must not be counted as a result")
	assert.Equal(t, "Naruto Kecil", got[0].Name)
	assert.Equal(t, srv.URL+"/anime/naruto-kecil/", got[0].URL)
	assert.Equal(t, sourceLabel, got[0].Source)
}

func TestSearchAnime_EmptyQuery(t *testing.T) {
	t.Parallel()
	_, c := newServer(t)
	_, err := c.SearchAnime(context.Background(), "")
	require.Error(t, err)
}

func TestGetAnimeEpisodes(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	eps, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/anime/naruto-kecil/")
	require.NoError(t, err)
	require.Len(t, eps, 2)
	assert.Equal(t, 1, eps[0].Num, "ascending")
	assert.Equal(t, "220", eps[1].Number)
	assert.Equal(t, "Naruto Kecil Episode 220", eps[1].Title.English)
}

func TestGetAnimeEpisodes_NoList(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	_, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/naruto-kecil-episode-220/")
	require.Error(t, err)
}

func TestGetEpisodeStreamURL(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	tests := []struct {
		name, quality, want string
	}{
		{"best skips the removed 1080p file and takes 720p", "best", srv.URL + "/api/file/UidvB5qB"},
		{"explicit 480p", "480p", srv.URL + "/api/file/Low480"},
		{"explicit 1080p is gone, falls back to highest live", "1080p", srv.URL + "/api/file/UidvB5qB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, meta, err := c.GetEpisodeStreamURL(context.Background(), srv.URL+"/naruto-kecil-episode-220/", tt.quality)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "samehadaku", meta["source"])
		})
	}
}

func TestQualities(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.Qualities(context.Background(), srv.URL+"/naruto-kecil-episode-220/")
	require.NoError(t, err)
	assert.Equal(t, []string{"1080p", "720p", "480p"}, got, "Pixeldrain heights only, highest first; Blogspot and Vidhide excluded")
}

// Blogspot is the fallback when no Pixeldrain server is listed.
func TestGetEpisodeStreamURL_BlogspotOnly(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/ep/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<div id="server"><ul><li><div class="east_player_option" data-post="1" data-nume="1" data-type="schtml"><span>Blogspot</span></div></li></ul></div>`))
	})
	mux.HandleFunc("/wp-admin/admin-ajax.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<iframe src="https://www.blogger.com/video.g?token=AD6v5dzYzfD2"></iframe>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	got, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/ep/", "best")
	require.NoError(t, err)
	assert.Equal(t, "https://www.blogger.com/video.g?token=AD6v5dzYzfD2", got)
}

func TestGetEpisodeStreamURL_NoServers(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<p>nothing</p>`)) }))
	t.Cleanup(srv.Close)
	_, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/ep/", "best")
	require.Error(t, err)
}

func TestGetEpisodeStreamURL_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(srv.Close)
	_, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/ep/", "best")
	require.Error(t, err)
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	c := NewSamehadakuClient()
	assert.Equal(t, "https://pixeldrain.com/api/file/abc123", c.resolveEmbed("https://pixeldrain.com/u/abc123"))
	assert.Equal(t, "https://www.blogger.com/video.g?token=x_y-Z", c.resolveEmbed("https://www.blogger.com/video.g?token=x_y-Z"))
	assert.Equal(t, "", c.resolveEmbed("https://krakenfiles.com/embed-video/x"))
	assert.Equal(t, 720, heightOf("Pixel 720p"))
	assert.Equal(t, 0, heightOf("Blogspot"))
	assert.Equal(t, "Naruto Kecil", cleanTitle("Naruto Kecil Sub Indo"))
}
