package samehadaku

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/KidiXDev/GonimeId/internal/scraper/netx"
	"github.com/KidiXDev/GonimeId/internal/util"
	"github.com/PuerkitoBio/goquery"
)

const (
	// samehadakuBase is the public host, pinned so a rotation fails loudly in
	// TestHostIsPinned instead of silently scraping the wrong site.
	samehadakuBase = "https://v2.samehadaku.how"
	pixeldrainBase = "https://pixeldrain.com"
	wibufileBase   = "https://api.wibufile.com"
	sourceLabel    = "Samehadaku"
	maxBodyBytes   = 8 << 20
)

var (
	episodeNumRe  = regexp.MustCompile(`(?i)episode[- ](\d+)`)
	iframeSrcRe   = regexp.MustCompile(`<iframe[^>]+src="([^"]+)"`)
	pixeldrainRe  = regexp.MustCompile(`pixeldrain\.com/(?:u|api/file)/([A-Za-z0-9]+)`)
	bloggerRe     = regexp.MustCompile(`https://www\.blogger\.com/video\.g\?token=[A-Za-z0-9_-]+`)
	wibufileAPIRe = regexp.MustCompile(`(?i)\burl\s*:\s*["']([^"']+)["']`)
	qualityRe     = regexp.MustCompile(`(\d{3,4})`)
	subIndoRe     = regexp.MustCompile(`(?i)\s*(?:subtitle|sub)\s+indo(?:nesia)?\s*$`)
)

// SamehadakuClient handles interactions with samehadaku.
type SamehadakuClient struct {
	client     *http.Client
	prober     *http.Client // plain net/http: file hosts (pixeldrain, googlevideo) omit ALPN and break surf's h2 path
	baseURL    string
	pixeldrain string // file host; overridden in tests so the probe stays offline
	wibufile   string // API host; overridden in tests so embed resolution stays offline
	maxRetries int
	retryDelay time.Duration
}

// NewSamehadakuClient performs no network I/O: it runs under sync.Once in the adapter.
func NewSamehadakuClient() *SamehadakuClient {
	return &SamehadakuClient{client: util.NewFastClient(), prober: newProber(), baseURL: samehadakuBase, pixeldrain: pixeldrainBase, wibufile: wibufileBase, maxRetries: 2, retryDelay: 300 * time.Millisecond}
}

// NewClientForTest points the client at a test server with retries disabled.
func NewClientForTest(serverURL string) *SamehadakuClient {
	c := NewSamehadakuClient()
	c.baseURL = strings.TrimSuffix(serverURL, "/")
	c.pixeldrain = c.baseURL
	c.wibufile = c.baseURL
	c.maxRetries, c.retryDelay = 0, 0
	c.prober = &http.Client{Timeout: 5 * time.Second} // the SSRF guard rejects loopback
	return c
}

// newProber builds the playability-probe client: stdlib net/http on the SSRF-
// guarded transport. It negotiates h2 or http/1.1 as the host offers, unlike
// the surf client, which insists on h2 and fails on pixeldrain/googlevideo
// with `negotiated ALPN "", expected h2`.
func newProber() *http.Client {
	return &http.Client{Transport: netx.SafeScraperTransport(8 * time.Second), Timeout: 10 * time.Second}
}

// fetch performs a GET (form == nil) or a form POST with the retry policy and
// returns the body. 5xx and transport errors retry; everything else is final.
func (c *SamehadakuClient) fetch(ctx context.Context, rawURL, layer string, form url.Values) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		var req *http.Request
		var err error
		if form == nil {
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
		} else {
			req, err = http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
		}
		if err != nil {
			return nil, netx.NewParserError(sourceLabel, layer, "bad request URL", err)
		}
		req.Header.Set("User-Agent", netx.UserAgent)
		req.Header.Set("Referer", c.baseURL+"/")
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("X-Requested-With", "XMLHttpRequest")
		}

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

func (c *SamehadakuClient) document(ctx context.Context, rawURL, layer string) (*goquery.Document, error) {
	body, err := c.fetch(ctx, rawURL, layer, nil)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, netx.NewParserError(sourceLabel, layer, "failed to parse page", err)
	}
	if err := netx.CheckChallengeDocument(doc, sourceLabel+" "+layer); err != nil {
		return nil, err
	}
	return doc, nil
}

// SearchAnime returns the anime cards of the first results page.
func (c *SamehadakuClient) SearchAnime(ctx context.Context, query string) ([]*models.Anime, error) {
	// CLI args arrive hyphenated ("naruto-kecil"); WordPress reads "-kecil" as
	// an exclusion and returns nothing, so hyphens go back to spaces (as Goyabu).
	query = strings.Join(strings.FieldsFunc(query, func(r rune) bool { return r == '-' || r == '_' || r == ' ' }), " ")
	if query == "" {
		return nil, netx.NewParserError(sourceLabel, "search", "empty query", nil)
	}
	doc, err := c.document(ctx, fmt.Sprintf("%s/?s=%s", c.baseURL, url.QueryEscape(query)), "search")
	if err != nil {
		return nil, err
	}
	var out []*models.Anime
	doc.Find("article.animpost div.animposx > a[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		title := cleanTitle(a.AttrOr("title", ""))
		if title == "" {
			title = cleanTitle(a.Find(".title").Text())
		}
		if !strings.Contains(href, "/anime/") || title == "" {
			return
		}
		out = append(out, &models.Anime{
			Name:      title,
			URL:       href,
			ImageURL:  a.Find("img").AttrOr("src", ""),
			Source:    sourceLabel,
			MediaType: models.MediaTypeAnime,
		})
	})
	return out, nil
}

// GetAnimeEpisodes lists an anime's episodes, ascending.
func (c *SamehadakuClient) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	doc, err := c.document(ctx, animeURL, "episodes")
	if err != nil {
		return nil, err
	}
	var eps []models.Episode
	doc.Find("div.lstepsiode ul li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("span.eps a").First()
		href := a.AttrOr("href", "")
		num, _ := strconv.Atoi(strings.TrimSpace(a.Text()))
		if num == 0 {
			if m := episodeNumRe.FindStringSubmatch(href); m != nil {
				num, _ = strconv.Atoi(m[1])
			}
		}
		if href == "" || num == 0 {
			return
		}
		eps = append(eps, models.Episode{
			Number: strconv.Itoa(num),
			Num:    num,
			URL:    href,
			Title:  models.TitleDetails{English: strings.TrimSpace(li.Find("span.lchx a").Text())},
		})
	})
	if len(eps) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no episodes found (page layout changed?)", nil)
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Num < eps[j].Num })
	return eps, nil
}

// server is one entry of the episode page's player list.
type server struct {
	post, nume, typ, label string
	direct                 string
	height                 int
}

// Qualities lists the resolutions the episode's players offer, highest first
// ("1080p", "720p", …). Blogspot carries no label and is left out.
func (c *SamehadakuClient) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var heights []int
	for _, s := range parseServers(doc) {
		if s.height > 0 && !seen[s.height] {
			seen[s.height] = true
			heights = append(heights, s.height)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	out := make([]string, len(heights))
	for i, h := range heights {
		out[i] = strconv.Itoa(h) + "p"
	}
	return out, nil
}

// GetEpisodeStreamURL resolves an episode to a direct file or Blogger URL.
// The site's Wibufile player goes first at each height; download mirrors and
// Blogspot remain fallbacks.
func (c *SamehadakuClient) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (streamURL string, metadata map[string]string, err error) {
	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return "", nil, err
	}
	servers := parseServers(doc)
	if len(servers) == 0 {
		return "", nil, netx.NewParserError(sourceLabel, "stream", "no player servers on episode page (layout changed?)", nil)
	}
	want := heightOf(quality)
	sort.SliceStable(servers, func(i, j int) bool {
		if want > 0 && (servers[i].height == want) != (servers[j].height == want) {
			return servers[i].height == want
		}
		return servers[i].height > servers[j].height
	})

	var lastErr error
	for _, s := range servers {
		u := c.resolveEmbed(s.direct)
		if u == "" {
			body, err := c.fetch(ctx, c.baseURL+"/wp-admin/admin-ajax.php", "player", url.Values{
				"action": {"player_ajax"}, "post": {s.post}, "nume": {s.nume}, "type": {s.typ},
			})
			if err != nil {
				lastErr = err
				continue
			}
			embed := iframeSrc(string(body))
			u = c.resolveEmbed(embed)
			if u == "" {
				u, err = c.resolveWibufile(ctx, embed)
				if err != nil {
					lastErr = err
					continue
				}
			}
		}
		if u == "" || (!strings.Contains(u, "blogger.com") && !c.playable(ctx, u)) {
			continue // unknown host, or a removed file: try the next server
		}
		util.Debug("Samehadaku server resolved", "label", s.label)
		return u, map[string]string{"source": "samehadaku", "referer": c.baseURL + "/", "user_agent": netx.UserAgent}, nil
	}
	return "", nil, netx.NewParserError(sourceLabel, "stream", "no resolvable server for this episode", lastErr)
}

// playable asks for the first byte of a file URL and reports whether the host
// serves it (200/206), so a deleted Pixeldrain file falls through to the next
// server instead of reaching mpv.
func (c *SamehadakuClient) playable(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- URL comes from the pinned host's player
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// parseServers keeps only the hosts this provider understands.
func parseServers(doc *goquery.Document) []server {
	var out []server
	doc.Find("#server .east_player_option").Each(func(_ int, d *goquery.Selection) {
		label := strings.TrimSpace(d.Text())
		lower := strings.ToLower(label)
		if !strings.Contains(lower, "pixel") && !strings.Contains(lower, "blogspot") && !strings.Contains(lower, "wibufile") {
			return
		}
		s := server{post: d.AttrOr("data-post", ""), nume: d.AttrOr("data-nume", ""), typ: d.AttrOr("data-type", ""), label: label, height: heightOf(label)}
		if s.post != "" && s.nume != "" {
			out = append(out, s)
		}
	})

	// Current pages expose Wibufile/Mega players, while their direct
	// per-resolution Pixeldrain files live in the download list.
	seen := map[int]bool{}
	doc.Find(".download-eps li").Each(func(_ int, li *goquery.Selection) {
		label := strings.TrimSpace(li.Find("strong").First().Text())
		height := heightOf(label)
		href := li.Find(`a[href*="pixeldrain.com/"]`).First().AttrOr("href", "")
		if height > 0 && href != "" && !seen[height] {
			seen[height] = true
			out = append(out, server{label: "Pixeldrain " + label, direct: href, height: height})
		}
	})
	return out
}

func (c *SamehadakuClient) resolveWibufile(ctx context.Context, src string) (string, error) {
	u, err := url.Parse(src)
	if err != nil {
		return "", netx.NewParserError(sourceLabel, "wibufile embed", "invalid URL", err)
	}
	if !c.isWibufile(u) {
		return "", nil
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".mp4") {
		return u.String(), nil
	}

	body, err := c.fetchWibufile(ctx, u.String(), c.baseURL+"/", "wibufile embed")
	if err != nil {
		return "", err
	}
	m := wibufileAPIRe.FindStringSubmatch(string(body))
	if m == nil {
		return "", nil
	}
	apiURL, err := u.Parse(html.UnescapeString(m[1]))
	if err != nil {
		return "", netx.NewParserError(sourceLabel, "wibufile API", "invalid URL", err)
	}
	if !c.isWibufile(apiURL) {
		return "", nil
	}
	body, err = c.fetchWibufile(ctx, apiURL.String(), u.String(), "wibufile API")
	if err != nil {
		return "", err
	}
	var response struct {
		Sources []struct {
			File string `json:"file"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", netx.NewParserError(sourceLabel, "wibufile API", "invalid response", err)
	}
	for _, source := range response.Sources {
		mediaURL, parseErr := apiURL.Parse(source.File)
		if parseErr == nil && c.isWibufile(mediaURL) {
			return mediaURL.String(), nil
		}
	}
	return "", nil
}

func (c *SamehadakuClient) fetchWibufile(ctx context.Context, rawURL, referer, layer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, netx.NewParserError(sourceLabel, layer, "bad request URL", err)
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Referer", referer)
	resp, err := c.prober.Do(req) // #nosec G704 -- URL is restricted to the Wibufile host
	if err != nil {
		return nil, netx.NewParserError(sourceLabel, layer, "request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, netx.NewHTTPStatusError(sourceLabel, layer, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, netx.NewParserError(sourceLabel, layer, "failed to read response", err)
	}
	return body, nil
}

func (c *SamehadakuClient) isWibufile(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	base, err := url.Parse(c.wibufile)
	if err != nil {
		return false
	}
	host, baseHost := strings.ToLower(u.Hostname()), strings.ToLower(base.Hostname())
	return host == baseHost || (baseHost == "api.wibufile.com" && strings.HasSuffix(host, ".wibufile.com"))
}

// resolveEmbed maps a server iframe to something the player can open, or "".
func (c *SamehadakuClient) resolveEmbed(src string) string {
	if m := pixeldrainRe.FindStringSubmatch(src); m != nil {
		return c.pixeldrain + "/api/file/" + m[1]
	}
	return bloggerRe.FindString(src)
}

func iframeSrc(markup string) string {
	if m := iframeSrcRe.FindStringSubmatch(markup); m != nil {
		return m[1]
	}
	return ""
}

// heightOf turns "Pixel 720p"/"720" into 720; "best"/"" → 0.
func heightOf(q string) int {
	if m := qualityRe.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// cleanTitle drops the site's "Sub Indo" suffix; tagging adds its own.
func cleanTitle(s string) string {
	return strings.TrimSpace(subIndoRe.ReplaceAllString(strings.TrimSpace(s), ""))
}
