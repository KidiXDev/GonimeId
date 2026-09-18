package otakudesu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHostIsPinned fails loudly if the base host rotates without a deliberate
// edit (dated 2026-09-18: otakudesu.cloud → .best → .blog).
func TestHostIsPinned(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://otakudesu.blog", otakudesuBase, "host rotated? update the const and this assertion together")
}

// Fixtures mirror the live markup captured 2026-09-18. File URLs point back at
// the test server so the playability probe never leaves the process.
const (
	searchHTML = `<ul class="chivsrc"><li><img src="/p.jpg"/><h2><a href="BASE/anime/borot-sub-indo/">Boruto: Naruto Next Generations Subtitle Indonesia</a></h2></li>
<li><h2><a href="BASE/genres/action/">Not an anime</a></h2></li></ul>`
	animeHTML = `<div class="episodelist"><ul><li><span><a href="BASE/batch/btr-batch-sub-indo/">Boruto [BATCH] Subtitle Indonesia</a></span></li></ul></div>
<div class="episodelist"><ul>
<li><span><a href="BASE/episode/btr-ng-episode-293-sub-indo/">Boruto Episode 293 Subtitle Indonesia</a></span></li>
<li><span><a href="BASE/episode/btr-ng-episode-1-sub-indo/">Boruto Episode 1 Subtitle Indonesia</a></span></li>
<li><span><a href="BASE/lengkap/btr-part-6/">Boruto Part 6</a></span></li>
<li><span><a href="BASE/episode/pembatas-episode-episode-1-900-dalam-proses/">Pembatas Episode 1-900</a></span></li>
</ul></div>`
	// odstream layout: playerjs config with an archive.org-style file.
	arcgEmbed = `<script>var vs = {id:"playerjs", file:"BASE/files/Boruto--293_720p.mp4"};</script>`
	// desudrive layout: JSON "file" key pointing at a Google Drive API URL.
	desudriveEmbed = `<script>var config = {"file":"BASE/files/gdrive?alt=media&amp;key=k","type":"mp4"};</script>`
	// updesu layout: a Blogger iframe the player unwraps itself (never probed).
	updesuEmbed = `<iframe id="myIframe" src="https://www.blogger.com/video.g?token=AD6v5dy2Hvko" allowfullscreen></iframe>`
	// otakuplay layout: <video><source> whose googlevideo URL is IP-locked (403).
	lockedEmbed = `<video controls><source src="BASE/files/locked?expire=1&amp;mime=video/mp4" type="video/mp4"></video>`
)

func mirrorAttr(i int, q string) string {
	b, _ := json.Marshal(map[string]any{"id": 138366, "i": i, "q": q})
	return base64.StdEncoding.EncodeToString(b)
}

// newServer serves the whole chain; embeds are served from the same host under
// a /desustream/ path so resolveEmbed's host check passes.
//
// Download section (tried first): Mp4 720p Pdrain → live; MKV 1080p Pdrain → removed (404).
// Mirrors:
//
//	360p i=0 otakuplay → locked (403)     i=1 odstream → arcg (ok)
//	480p i=0 updesu    → Blogger (via desustream)   i=1 blogs → Blogger iframe directly
//	720p i=0 otakustream → locked (403)   i=1 desudrive → gdrive (ok)   i=2 kraken (skipped host)
func newServer(t *testing.T) (*httptest.Server, *OtakudesuClient) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	base := func() string { return srv.URL }
	embeds := map[string]string{ // ajax (q,i) → embed path
		"360p/0": "/desustream/otakuplay", "360p/1": "/desustream/arcg",
		"480p/0": "/desustream/updesu", "480p/1": "https://www.blogger.com/video.g?token=DirectBlogs",
		"720p/0": "/desustream/otakustream", "720p/1": "/desustream/desudrive", "720p/2": "/kraken/embed",
	}
	episodeHTML := func() string {
		return `<div class="download"><ul>
<li><strong>Mp4 720p</strong> <a href="` + base() + `/go?to=Mp4720">Filedon</a> <a href="` + base() + `/go?to=Pd720">Pdrain</a> <a href="` + base() + `/go?to=x">Mega</a> <i>137 MB</i></li>
<li><strong>MKV 1080p</strong> <a href="` + base() + `/go?to=Fd1080">Filedon</a> <a href="` + base() + `/go?to=Gone1080">Pdrain</a> <i>240 MB</i></li>
</ul></div>
<div class="mirrorstream">
<ul class="m360p"><li><a href="#" data-content="` + mirrorAttr(0, "360p") + `">otakuplay</a></li><li><a href="#" data-content="` + mirrorAttr(1, "360p") + `">odstream</a></li></ul>
<ul class="m480p"><li><a href="#" data-content="` + mirrorAttr(0, "480p") + `">updesu</a></li><li><a href="#" data-content="` + mirrorAttr(1, "480p") + `">blogs</a></li></ul>
<ul class="m720p"><li><a href="#" data-content="` + mirrorAttr(0, "720p") + `">otakustream</a></li><li><a href="#" data-content="` + mirrorAttr(1, "720p") + `">desudrive</a></li><li><a href="#" data-content="` + mirrorAttr(2, "720p") + `">kraken</a></li></ul></div>
<iframe src="` + base() + `/desustream/updesu"></iframe>
<script>$.ajax(u,{data:{...e,nonce:window.__x__nonce,action:"2a3505c93b0035d3f455df82bf976b84"}});$.ajax(u,{data:{action:"aa1208d27f29ca340c92c66d1926f13f"}});</script>`
	}
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(replaceBase(body, base()))) }
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") != "" {
			serve(searchHTML)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/anime/borot-sub-indo/", serve(animeHTML))
	mux.HandleFunc("/episode/ep-293/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(episodeHTML())) })
	mux.HandleFunc("/desustream/updesu", serve(updesuEmbed))
	mux.HandleFunc("/desustream/arcg", serve(arcgEmbed))
	mux.HandleFunc("/desustream/desudrive", serve(desudriveEmbed))
	mux.HandleFunc("/desustream/otakuplay", serve(lockedEmbed))
	mux.HandleFunc("/desustream/otakustream", serve(lockedEmbed))
	// link.desustream.com stand-in: one 302 hop to the Pixeldrain page.
	mux.HandleFunc("/go", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base()+"/u/"+r.URL.Query().Get("to"), http.StatusFound)
	})
	mux.HandleFunc("/api/file/Gone1080", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/api/file/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"), "probe must ask for one byte only")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	})
	mux.HandleFunc("/files/locked", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"), "probe must ask for one byte only")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	})
	mux.HandleFunc("/wp-admin/admin-ajax.php", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.Form.Get("action") {
		case "aa1208d27f29ca340c92c66d1926f13f":
			_, _ = w.Write([]byte(`{"data":"6004abb725"}`))
		case "2a3505c93b0035d3f455df82bf976b84":
			require.Equal(t, "6004abb725", r.Form.Get("nonce"))
			target := embeds[r.Form.Get("q")+"/"+r.Form.Get("i")]
			require.NotEmpty(t, target, "unexpected mirror %s/%s", r.Form.Get("q"), r.Form.Get("i"))
			if !strings.HasPrefix(target, "https://") {
				target = base() + target // desustream-style embed served by this test server
			}
			iframe := base64.StdEncoding.EncodeToString([]byte(`<div><iframe src="` + target + `"></iframe></div>`))
			_ = json.NewEncoder(w).Encode(map[string]string{"data": iframe})
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, NewClientForTest(srv.URL)
}

func replaceBase(s, base string) string { return strings.ReplaceAll(s, "BASE", base) }

func TestSearchAnime(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.SearchAnime(context.Background(), "boruto")
	require.NoError(t, err)
	require.Len(t, got, 1, "the genre link must not be counted as a result")
	assert.Equal(t, "Boruto: Naruto Next Generations", got[0].Name, "Subtitle Indonesia suffix stripped")
	assert.Equal(t, srv.URL+"/anime/borot-sub-indo/", got[0].URL)
	assert.Equal(t, sourceLabel, got[0].Source)
}

func TestSearchAnime_EmptyQuery(t *testing.T) {
	t.Parallel()
	_, c := newServer(t)
	_, err := c.SearchAnime(context.Background(), "  ")
	require.Error(t, err)
}

func TestGetAnimeEpisodes(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	eps, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/anime/borot-sub-indo/")
	require.NoError(t, err)
	require.Len(t, eps, 2, "batch, lengkap and pembatas (separator) links are not episodes")
	assert.Equal(t, 1, eps[0].Num, "ascending")
	assert.Equal(t, "293", eps[1].Number)
	assert.Equal(t, srv.URL+"/episode/btr-ng-episode-1-sub-indo/", eps[0].URL)
}

func TestGetAnimeEpisodes_NoList(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	_, err := c.GetAnimeEpisodes(context.Background(), srv.URL+"/episode/ep-293/")
	require.Error(t, err)
}

func TestGetEpisodeStreamURL(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	tests := []struct {
		name, quality, want string
	}{
		{"best: 1080p download is gone, 720p download wins over every mirror", "best", srv.URL + "/api/file/Pd720"},
		{"explicit 720p prefers the download file", "720p", srv.URL + "/api/file/Pd720"},
		{"explicit 1080p is gone, falls back to the 720p download", "1080p", srv.URL + "/api/file/Pd720"},
		// Heights the download section lacks are still served by the mirrors,
		// but only after every download candidate was tried.
		{"480p: downloads have no 480p, tries them by height then the Blogger mirror", "480p", srv.URL + "/api/file/Pd720"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, meta, err := c.GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/ep-293/", tt.quality)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "otakudesu", meta["source"])
		})
	}
}

func TestQualities(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	got, err := c.Qualities(context.Background(), srv.URL+"/episode/ep-293/")
	require.NoError(t, err)
	assert.Equal(t, []string{"1080p", "720p", "480p", "360p"}, got, "download and mirror heights merged, highest first")
}

// With no download section the mirror logic stands on its own: locked mirrors
// are skipped, Blogger is not probed, unavailable heights fall back.
func TestGetEpisodeStreamURL_MirrorsOnly(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	var mirrorsOnly *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/episode/m/", func(w http.ResponseWriter, _ *http.Request) {
		resp, err := http.Get(srv.URL + "/episode/ep-293/") // #nosec G107 -- test server
		require.NoError(t, err)
		defer resp.Body.Close()
		page, _ := io.ReadAll(resp.Body)
		i := strings.Index(string(page), `<div class="mirrorstream">`)
		_, _ = w.Write(page[i:]) // drop the download section
	})
	mirrorsOnly = httptest.NewServer(mux)
	t.Cleanup(mirrorsOnly.Close)
	tests := []struct {
		name, quality, want string
	}{
		{"best skips the IP-locked 720p mirror and takes the next 720p", "best", srv.URL + "/files/gdrive?alt=media&key=k"},
		{"480p resolves the Blogger embed without probing it", "480p", "https://www.blogger.com/video.g?token=AD6v5dy2Hvko"},
		{"360p skips the locked mirror and takes odstream", "360p", srv.URL + "/files/Boruto--293_720p.mp4"},
		{"unavailable height falls back to highest", "1080p", srv.URL + "/files/gdrive?alt=media&key=k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := c.GetEpisodeStreamURL(context.Background(), mirrorsOnly.URL+"/episode/m/", tt.quality)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A mirror whose ajax answer is a Blogger iframe (the "blogs" mirror) is
// returned as is — it never goes through the desustream host check.
func TestGetEpisodeStreamURL_DirectBloggerMirror(t *testing.T) {
	t.Parallel()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/episode/x/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<div class="mirrorstream"><ul class="m480p"><li><a href="#" data-content="` + mirrorAttr(0, "480p") + `">blogs</a></li></ul></div>
<script>$.ajax(u,{data:{...e,nonce:window.__x__nonce,action:"2a3505c93b0035d3f455df82bf976b84"}});$.ajax(u,{data:{action:"aa1208d27f29ca340c92c66d1926f13f"}});</script>`))
	})
	mux.HandleFunc("/wp-admin/admin-ajax.php", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.Form.Get("action") == "aa1208d27f29ca340c92c66d1926f13f" {
			_, _ = w.Write([]byte(`{"data":"n"}`))
			return
		}
		iframe := base64.StdEncoding.EncodeToString([]byte(`<iframe src="https://www.blogger.com/video.g?token=DirectBlogs" allowfullscreen></iframe>`))
		_ = json.NewEncoder(w).Encode(map[string]string{"data": iframe})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	got, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/x/", "best")
	require.NoError(t, err)
	assert.Equal(t, "https://www.blogger.com/video.g?token=DirectBlogs", got)
}

// The page's own default iframe can be a Blogger embed too (tykmt episode 1).
func TestGetEpisodeStreamURL_DefaultIframeIsBlogger(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<iframe src="https://www.blogger.com/video.g?token=PageDefault" WIDTH="420"></iframe>`))
	}))
	t.Cleanup(srv.Close)
	got, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/x/", "best")
	require.NoError(t, err)
	assert.Equal(t, "https://www.blogger.com/video.g?token=PageDefault", got)
}

// Without the ajax action hashes the client can still use the page's own iframe.
func TestGetEpisodeStreamURL_DefaultIframeFallback(t *testing.T) {
	t.Parallel()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/episode/x/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<iframe src="` + srv.URL + `/desustream/updesu"></iframe>`))
	})
	mux.HandleFunc("/desustream/updesu", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(updesuEmbed)) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClientForTest(srv.URL)
	got, _, err := c.GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/x/", "best")
	require.NoError(t, err)
	assert.Equal(t, "https://www.blogger.com/video.g?token=AD6v5dy2Hvko", got)
}

func TestGetEpisodeStreamURL_NoMirrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<p>nothing here</p>`)) }))
	t.Cleanup(srv.Close)
	_, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/x/", "best")
	require.Error(t, err)
}

func TestGetEpisodeStreamURL_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(srv.Close)
	_, _, err := NewClientForTest(srv.URL).GetEpisodeStreamURL(context.Background(), srv.URL+"/episode/x/", "best")
	require.Error(t, err)
}

func TestHelpers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 720, heightOf("720p"))
	assert.Equal(t, 0, heightOf("best"))
	assert.Equal(t, 293, episodeNumber("/episode/btr-ng-episode-293-sub-indo/", ""))
	assert.Equal(t, 12, episodeNumber("/episode/x/", "Naruto Episode 12 Subtitle Indonesia"))
	assert.Equal(t, 0, episodeNumber("/batch/x/", "Naruto Batch"))
	assert.Equal(t, "Naruto", cleanTitle("Naruto Subtitle Indonesia"))
	assert.Equal(t, "Naruto", cleanTitle("Naruto Sub Indo"))
	assert.Equal(t, "", iframeSrc("<p>no iframe</p>"))
}
