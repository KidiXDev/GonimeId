package astronime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductionHostsArePinned(t *testing.T) {
	require.Equal(t, "https://astronime.id", astronimeBase)
	require.Equal(t, "https://enc-dec.app/api/dec-abyss", abyssDecrypt)
}

func TestAstronimeFlow(t *testing.T) {
	var serverURL string
	var decryptCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && r.URL.Query().Get("s") == "frieren":
			fmt.Fprintf(w, `<article class="animpost"><div class="animposx"><a href="%s/frieren/" title="Frieren"><img data-src="%s/frieren.jpg"></a></div></article>`, serverURL, serverURL)
		case r.URL.Path == "/frieren/":
			fmt.Fprintf(w, `<a href="%s/frieren-episode-02/">Episode 2</a><a href="%s/frieren-episode-01/">Episode 1</a>`, serverURL, serverURL)
		case r.URL.Path == "/frieren-episode-01/":
			fmt.Fprint(w, `<div class="east_player_option" data-post="42" data-nume="2">Hydrax</div>`)
		case r.URL.Path == "/wp-admin/admin-ajax.php":
			require.NoError(t, r.ParseForm())
			require.Equal(t, "player_ajax", r.Form.Get("action"))
			require.Equal(t, "42", r.Form.Get("post"))
			require.Equal(t, "2", r.Form.Get("nume"))
			fmt.Fprintf(w, `<iframe src="%s/hydrax/embed"></iframe>`, serverURL)
		case r.URL.Path == "/hydrax/embed":
			fmt.Fprint(w, `<script>const datas = "encrypted-player-data";</script>`)
		case r.URL.Path == "/decrypt":
			decryptCalls.Add(1)
			require.Equal(t, "https://playhydrax.com", r.Header.Get("Origin"))
			require.Equal(t, "https://playhydrax.com/", r.Header.Get("Referer"))
			fmt.Fprintf(w, `{"status":200,"result":{"sources":[{"url":"%s/media/480.mp4","type":"480p","status":true},{"url":"%s/media/1080.mp4","type":"1080p","status":true},{"url":"%s/media/720.mp4","type":"720p","status":true}]}}`, serverURL, serverURL, serverURL)
		case strings.HasPrefix(r.URL.Path, "/media/"):
			require.Equal(t, "bytes=0-0", r.Header.Get("Range"))
			require.Equal(t, serverURL+"/hydrax/embed", r.Header.Get("Referer"))
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL
	c := NewClientForTest(serverURL)
	ctx := context.Background()

	results, err := c.SearchAnime(ctx, "frieren")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "Frieren", results[0].Name)

	episodes, err := c.GetAnimeEpisodes(ctx, results[0].URL)
	require.NoError(t, err)
	require.Len(t, episodes, 2)
	require.Equal(t, 1, episodes[0].Num)

	qualities, err := c.Qualities(ctx, episodes[0].URL)
	require.NoError(t, err)
	require.Equal(t, []string{"1080p", "720p", "480p"}, qualities)

	streamURL, metadata, err := c.GetEpisodeStreamURL(ctx, episodes[0].URL, "720p")
	require.NoError(t, err)
	require.Equal(t, serverURL+"/media/720.mp4", streamURL)
	require.Equal(t, "astronime", metadata["source"])
	require.Equal(t, serverURL+"/hydrax/embed", metadata["referer"])
	require.Equal(t, int32(1), decryptCalls.Load())
}
