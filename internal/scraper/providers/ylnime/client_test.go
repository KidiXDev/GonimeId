package ylnime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostIsPinned(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://ylnime.com", ylnimeBase, "host rotated? update the const and this assertion together")
}

func newServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("search") != "":
			_, _ = w.Write([]byte(`<div class="card"><img src="/cover.jpg"><a href="?series=naruto" class="stretched-link"></a><h6 class="card-title">Naruto</h6></div>`))
		case r.URL.Query().Get("episode") != "":
			quality := r.URL.Query().Get("reso")
			if quality == "" {
				quality = "720p"
			}
			_, _ = w.Write([]byte(`<a href="?series=naruto&episode=naruto-1&reso=480p">480p</a><a href="?series=naruto&episode=naruto-1&reso=720p">720p</a><script>const streams = [{"reso":"` + quality + `","link":"` + srv.URL + `/gone.mp4"},{"reso":"` + quality + `","link":"` + srv.URL + `/video.mp4"}];</script>`))
		case r.URL.Query().Get("series") != "":
			_, _ = w.Write([]byte(`<div class="list-group"><a class="list-group-item" href="?series=naruto&episode=naruto-2"><span>Episode 2</span></a><a class="list-group-item" href="?series=naruto&episode=naruto-1"><span>Episode 1</span></a></div>`))
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/gone.mp4", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/video.mp4", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=0-0", r.Header.Get("Range"))
		w.WriteHeader(http.StatusPartialContent)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, NewClientForTest(srv.URL)
}

func TestClientFlow(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)

	results, err := c.SearchAnime(context.Background(), "naruto")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Naruto", results[0].Name)
	assert.Equal(t, sourceLabel, results[0].Source)
	assert.Equal(t, srv.URL+"/?series=naruto", results[0].URL)

	episodes, err := c.GetAnimeEpisodes(context.Background(), results[0].URL)
	require.NoError(t, err)
	require.Len(t, episodes, 2)
	assert.Equal(t, 1, episodes[0].Num)
	assert.Equal(t, "Episode 2", episodes[1].Title.English)

	qualities, err := c.Qualities(context.Background(), episodes[0].URL)
	require.NoError(t, err)
	assert.Equal(t, []string{"720p", "480p"}, qualities)

	streamURL, metadata, err := c.GetEpisodeStreamURL(context.Background(), episodes[0].URL, "480p")
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/video.mp4", streamURL)
	assert.Equal(t, "ylnime", metadata["source"])
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	srv, c := newServer(t)
	_, err := c.SearchAnime(context.Background(), "  ")
	require.Error(t, err)
	_, err = c.GetAnimeEpisodes(context.Background(), srv.URL+"/")
	require.Error(t, err)
	_, _, err = c.GetEpisodeStreamURL(context.Background(), srv.URL+"/?series=x&episode=x", "bad")
	require.NoError(t, err, "unknown quality is passed through; the source may normalize it")
	_, err = parseStreams([]byte(`<script>const streams = [];</script>`))
	require.Error(t, err)
	assert.Empty(t, c.absolute(""))
	assert.True(t, strings.Contains(c.absolute("?series=x"), "series=x"))
}
