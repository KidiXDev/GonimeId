package moenime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostIsPinned(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://moenime.com", moenimeBase)
	assert.Equal(t, "https://moeclip.com", moeclipBase)
}

func TestClientFlow(t *testing.T) {
	t.Parallel()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "saijo no osewa", r.URL.Query().Get("s"))
		_, _ = w.Write([]byte(`<article><div class="featured-thumb"><img src="/cover.jpg"></div><h1 class="entry-title"><a href="/anime/">Saijo no Osewa Sub Indo</a></h1></article>`))
	})
	mux.HandleFunc("/anime/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<table>
<td id="02"><a class="moe-stream-a" href="` + srv.URL + `/clip/saijo/02/">Stream</a></td>
<td id="01"><a class="moe-stream-a" href="` + srv.URL + `/clip/saijo/01/">Stream</a></td>
</table>`))
	})
	mux.HandleFunc("/clip/saijo/01/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<div id="down-title">115387</div>
<button class="mirrorlist aktif" meta-src="mediaID"></button>
<select id="source"><option value="240p">240p</option><option value="720p">720p</option><option value="480p">480p</option></select>`))
	})
	mux.HandleFunc("/v/mediaID_720p_115387.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<script>player.setup({file: '` + srv.URL + `/media/video.mp4?token=kept'});</script>`))
	})
	mux.HandleFunc("/media/video.mp4", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"))
		require.Equal(t, "kept", r.URL.Query().Get("token"))
		w.WriteHeader(http.StatusPartialContent)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClientForTest(srv.URL)

	results, err := c.SearchAnime(context.Background(), "saijo-no-osewa")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Saijo no Osewa", results[0].Name)
	assert.Equal(t, srv.URL+"/cover.jpg", results[0].ImageURL)

	episodes, err := c.GetAnimeEpisodes(context.Background(), results[0].URL)
	require.NoError(t, err)
	require.Len(t, episodes, 2)
	assert.Equal(t, 1, episodes[0].Num)
	assert.Equal(t, srv.URL+"/clip/saijo/01/", episodes[0].URL)

	qualities, err := c.Qualities(context.Background(), episodes[0].URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"720p", "480p", "240p"}, qualities)

	streamURL, metadata, err := c.GetEpisodeStreamURL(context.Background(), episodes[0].URL, "720p")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/media/video.mp4?token=kept", streamURL)
	assert.Equal(t, "moenime", metadata["source"])
	assert.Equal(t, srv.URL+"/", metadata["referer"])
}
