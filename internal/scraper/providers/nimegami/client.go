package nimegami

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/scraper/netx"
	"github.com/KidiXDev/GonimeId/internal/util"
	"github.com/PuerkitoBio/goquery"
)

const (
	// nimegamiBase is the public host, pinned so a rotation fails loudly in
	// TestHostIsPinned instead of silently scraping the wrong site.
	nimegamiBase = "https://nimegami.id"
	sourceLabel  = "Nimegami"
	maxBodyBytes = 8 << 20
	// episodeFragment is the URL fragment that names an episode on the season page.
	episodeFragment = "#play_eps_"
)

var (
	episodeIDRe = regexp.MustCompile(`^play_eps_(\d+)$`)
	sourceSrcRe = regexp.MustCompile(`<source[^>]+src=["']([^"']+)["']`)
	streamAPIRe = regexp.MustCompile(`STREAM_URL_API\s*=\s*["']([^"']+)["']`)
	qualityRe   = regexp.MustCompile(`(\d{3,4})`)
	// titleNoiseRe drops the site's "Sub Indo" and ": Episode 1 – 28 (End)" suffixes.
	titleNoiseRe = regexp.MustCompile(`(?i)\s*(?:sub\s+indo|subtitle\s+indonesia)?\s*(?::\s*episode.*)?$`)
)

// NimegamiClient handles interactions with nimegami.id.
type NimegamiClient struct {
	client     *http.Client
	prober     *http.Client // plain net/http: file CDNs omit ALPN and break surf's h2 path
	baseURL    string
	maxRetries int
	retryDelay time.Duration

	// The season page is ~230 KB and is needed by episodes, qualities and
	// stream resolution in a row; the last one fetched is kept.
	pageMu   sync.Mutex
	pageURL  string
	pageBody []byte
}

// NewNimegamiClient performs no network I/O: it runs under sync.Once in the adapter.
func NewNimegamiClient() *NimegamiClient {
	return &NimegamiClient{client: util.NewFastClient(), prober: newProber(), baseURL: nimegamiBase, maxRetries: 2, retryDelay: 300 * time.Millisecond}
}

// NewClientForTest points the client at a test server with retries disabled.
func NewClientForTest(serverURL string) *NimegamiClient {
	c := NewNimegamiClient()
	c.baseURL = strings.TrimSuffix(serverURL, "/")
	c.maxRetries, c.retryDelay = 0, 0
	c.prober = &http.Client{Timeout: 5 * time.Second} // the SSRF guard rejects loopback
	return c
}

// newProber builds the playability-probe client: stdlib net/http on the SSRF-
// guarded transport, which negotiates h2 or http/1.1 as the host offers.
func newProber() *http.Client {
	return &http.Client{Transport: netx.SafeScraperTransport(8 * time.Second), Timeout: 10 * time.Second}
}

// fetch performs a GET with the retry policy and returns the body. 5xx and
// transport errors retry; everything else is final.
func (c *NimegamiClient) fetch(ctx context.Context, rawURL, layer string) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
		if err != nil {
			return nil, netx.NewParserError(sourceLabel, layer, "bad request URL", err)
		}
		req.Header.Set("User-Agent", netx.UserAgent)
		req.Header.Set("Referer", c.baseURL+"/")

		resp, err := c.client.Do(req) // #nosec G704 -- URL is built from the pinned base
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
			_ = resp.Body.Close()
			switch {
			case readErr != nil:
				err = readErr
			case resp.StatusCode == http.StatusOK:
				return body, nil
			case resp.StatusCode < 500:
				return nil, netx.NewHTTPStatusError(sourceLabel, layer, resp.StatusCode)
			default:
				err = netx.NewHTTPStatusError(sourceLabel, layer, resp.StatusCode)
			}
		}
		lastErr = err
		if attempt >= c.maxRetries {
			return nil, netx.NewParserError(sourceLabel, layer, "request failed", lastErr)
		}
		if !sleepCtx(ctx, c.retryDelay) {
			return nil, ctx.Err()
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func parseDoc(body []byte, layer string) (*goquery.Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, netx.NewParserError(sourceLabel, layer, "failed to parse page", err)
	}
	if err := netx.CheckChallengeDocument(doc, sourceLabel+" "+layer); err != nil {
		return nil, err
	}
	return doc, nil
}

// seasonPage returns the parsed season page, served from the one-entry cache
// when the same page was fetched last.
func (c *NimegamiClient) seasonPage(ctx context.Context, pageURL, layer string) (*goquery.Document, error) {
	c.pageMu.Lock()
	body, hit := c.pageBody, c.pageURL == pageURL && c.pageBody != nil
	c.pageMu.Unlock()
	if !hit {
		var err error
		if body, err = c.fetch(ctx, pageURL, layer); err != nil {
			return nil, err
		}
		c.pageMu.Lock()
		c.pageURL, c.pageBody = pageURL, body
		c.pageMu.Unlock()
	}
	return parseDoc(body, layer)
}

// SearchAnime returns the cards of the first results page.
func (c *NimegamiClient) SearchAnime(ctx context.Context, query string) ([]*models.Anime, error) {
	// CLI args arrive hyphenated ("sousou-no-frieren"); WordPress reads "-x"
	// as an exclusion and returns nothing, so hyphens go back to spaces.
	query = strings.Join(strings.FieldsFunc(query, func(r rune) bool { return r == '-' || r == '_' || r == ' ' }), " ")
	if query == "" {
		return nil, netx.NewParserError(sourceLabel, "search", "empty query", nil)
	}
	body, err := c.fetch(ctx, fmt.Sprintf("%s/?s=%s", c.baseURL, url.QueryEscape(query)), "search")
	if err != nil {
		return nil, err
	}
	doc, err := parseDoc(body, "search")
	if err != nil {
		return nil, err
	}
	var out []*models.Anime
	doc.Find("article").Each(func(_ int, art *goquery.Selection) {
		a := art.Find("h2 a").First()
		href, title := a.AttrOr("href", ""), cleanTitle(a.Text())
		if href == "" || title == "" {
			return
		}
		out = append(out, &models.Anime{
			Name:      title,
			URL:       href,
			ImageURL:  art.Find("img").AttrOr("src", ""),
			Source:    sourceLabel,
			MediaType: models.MediaTypeAnime,
		})
	})
	return out, nil
}

// streamEntry is one resolution of an episode's decoded payload.
type streamEntry struct {
	Format string   `json:"format"`
	URLs   []string `json:"url"`
}

// episodePayloads reads every li.select-eps[data] on the season page:
// episode number → decoded resolutions, plus the row's title.
func episodePayloads(doc *goquery.Document) (map[int][]streamEntry, map[int]string) {
	streams, titles := map[int][]streamEntry{}, map[int]string{}
	doc.Find("li.select-eps[data]").Each(func(_ int, li *goquery.Selection) {
		m := episodeIDRe.FindStringSubmatch(li.AttrOr("id", ""))
		if m == nil {
			return
		}
		num, _ := strconv.Atoi(m[1])
		raw, err := base64.StdEncoding.DecodeString(li.AttrOr("data", ""))
		if err != nil {
			return
		}
		var entries []streamEntry
		if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
			return
		}
		streams[num] = entries
		titles[num] = cleanTitle(li.AttrOr("title", ""))
	})
	return streams, titles
}

// GetAnimeEpisodes lists a season page's episodes, ascending. Each episode
// URL is the page URL plus a #play_eps_<n> fragment.
func (c *NimegamiClient) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	pageURL, _ := splitEpisodeURL(animeURL)
	doc, err := c.seasonPage(ctx, pageURL, "episodes")
	if err != nil {
		return nil, err
	}
	streams, titles := episodePayloads(doc)
	if len(streams) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no episodes found (page layout changed?)", nil)
	}
	eps := make([]models.Episode, 0, len(streams))
	for num := range streams {
		eps = append(eps, models.Episode{
			Number: strconv.Itoa(num),
			Num:    num,
			URL:    pageURL + episodeFragment + strconv.Itoa(num),
			Title:  models.TitleDetails{English: titles[num]},
		})
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Num < eps[j].Num })
	return eps, nil
}

// Qualities lists the episode's resolutions, highest first.
func (c *NimegamiClient) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	entries, err := c.episodeStreams(ctx, episodeURL)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var heights []int
	for _, e := range entries {
		if h := heightOf(e.Format); h > 0 && !seen[h] {
			seen[h] = true
			heights = append(heights, h)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	out := make([]string, len(heights))
	for i, h := range heights {
		out[i] = strconv.Itoa(h) + "p"
	}
	return out, nil
}

// GetEpisodeStreamURL resolves an episode to a direct mp4. quality is
// "best"/"" or a label such as "720p"; the requested height is tried first,
// then the rest by height descending. Every file is probed before it is
// returned so a removed upload falls through to the next server.
func (c *NimegamiClient) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (streamURL string, metadata map[string]string, err error) {
	entries, err := c.episodeStreams(ctx, episodeURL)
	if err != nil {
		return "", nil, err
	}
	want := heightOf(quality)
	sort.SliceStable(entries, func(i, j int) bool {
		hi, hj := heightOf(entries[i].Format), heightOf(entries[j].Format)
		if want > 0 && (hi == want) != (hj == want) {
			return hi == want
		}
		return hi > hj
	})
	var lastErr error
	for _, e := range entries {
		for _, embed := range e.URLs {
			u, err := c.resolveEmbed(ctx, embed)
			if err != nil {
				lastErr = err
				continue
			}
			util.Debug("Nimegami stream resolved", "quality", e.Format)
			return u, map[string]string{"source": "nimegami", "referer": c.baseURL + "/"}, nil
		}
	}
	return "", nil, netx.NewParserError(sourceLabel, "stream", "no playable server for this episode", lastErr)
}

// episodeStreams fetches the season page and returns the named episode's
// decoded resolutions.
func (c *NimegamiClient) episodeStreams(ctx context.Context, episodeURL string) ([]streamEntry, error) {
	pageURL, num := splitEpisodeURL(episodeURL)
	if num == 0 {
		return nil, netx.NewParserError(sourceLabel, "identity", fmt.Sprintf("not a nimegami episode URL: %s", episodeURL), nil)
	}
	doc, err := c.seasonPage(ctx, pageURL, "episode")
	if err != nil {
		return nil, err
	}
	streams, _ := episodePayloads(doc)
	entries, ok := streams[num]
	if !ok {
		return nil, netx.NewParserError(sourceLabel, "episode", fmt.Sprintf("episode %d is not on the page", num), nil)
	}
	return append([]streamEntry(nil), entries...), nil
}

// resolveEmbed fetches a streaming page, resolves its media URL and probes it.
func (c *NimegamiClient) resolveEmbed(ctx context.Context, embedURL string) (string, error) {
	body, err := c.fetch(ctx, embedURL, "embed")
	if err != nil {
		return "", err
	}
	if m := streamAPIRe.FindSubmatch(body); m != nil {
		base, err := url.Parse(embedURL)
		if err != nil {
			return "", netx.NewParserError(sourceLabel, "embed", "bad streaming page URL", err)
		}
		endpoint, err := url.Parse(string(m[1]))
		if err != nil {
			return "", netx.NewParserError(sourceLabel, "embed", "bad stream API URL", err)
		}
		apiBody, err := c.fetch(ctx, base.ResolveReference(endpoint).String(), "stream API")
		if err != nil {
			return "", err
		}
		var payload struct {
			OK  bool   `json:"ok"`
			URL string `json:"url"`
		}
		if err := json.Unmarshal(apiBody, &payload); err != nil || !payload.OK || payload.URL == "" {
			return "", netx.NewParserError(sourceLabel, "stream API", "no media URL in response", err)
		}
		if !c.playable(ctx, payload.URL) {
			return "", netx.NewParserError(sourceLabel, "stream API", "file not served from here", nil)
		}
		return payload.URL, nil
	}
	m := sourceSrcRe.FindSubmatch(body)
	if m == nil {
		return "", netx.NewParserError(sourceLabel, "embed", "no <source> in streaming page (layout changed?)", nil)
	}
	fileURL := string(m[1])
	if i := strings.Index(fileURL, "?"); i >= 0 {
		fileURL = fileURL[:i]
	}
	if !c.playable(ctx, fileURL) {
		return "", netx.NewParserError(sourceLabel, "embed", "file not served from here", nil)
	}
	return fileURL, nil
}

// playable asks for the first byte of a file URL and reports whether the host
// serves it (200/206), following the CDN's redirect.
func (c *NimegamiClient) playable(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- URL comes from the pinned host's player page
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// splitEpisodeURL separates "<page>#play_eps_<n>" into the page and n (0 when absent).
func splitEpisodeURL(episodeURL string) (pageURL string, num int) {
	pageURL, frag, _ := strings.Cut(strings.TrimSpace(episodeURL), "#")
	if m := episodeIDRe.FindStringSubmatch(frag); m != nil {
		num, _ = strconv.Atoi(m[1])
	}
	return pageURL, num
}

// heightOf turns "720p"/"720" into 720; "best"/"" → 0.
func heightOf(q string) int {
	if m := qualityRe.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// cleanTitle drops "Sub Indo" and the ": Episode 1 – 28 (End)" suffix.
func cleanTitle(s string) string {
	return strings.TrimSpace(titleNoiseRe.ReplaceAllString(strings.TrimSpace(s), ""))
}
