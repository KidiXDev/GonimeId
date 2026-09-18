package otakudesu

import (
	"context"
	"encoding/base64"
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
	// otakudesuBase is the public host, pinned so a rotation fails loudly in
	// TestHostIsPinned instead of silently scraping the wrong site.
	otakudesuBase  = "https://otakudesu.blog"
	pixeldrainBase = "https://pixeldrain.com"
	sourceLabel    = "Otakudesu"
	maxBodyBytes   = 8 << 20
)

var (
	episodeNumRe = regexp.MustCompile(`(?i)episode[- ](\d+)`)
	// nonceActionRe / embedActionRe read the two ajax action hashes out of the
	// episode page's inline script; they are opaque and may rotate per deploy.
	nonceActionRe  = regexp.MustCompile(`data:\{action:"([0-9a-f]{32})"\}`)
	embedActionRe  = regexp.MustCompile(`nonce:[^,]+,action:"([0-9a-f]{32})"`)
	iframeSrcRe    = regexp.MustCompile(`<iframe[^>]+src="([^"]+)"`)
	bloggerRe      = regexp.MustCompile(`https://www\.blogger\.com/video\.g\?token=[A-Za-z0-9_-]+`)
	pixeldrainIDRe = regexp.MustCompile(`^/(?:u|api/file)/([A-Za-z0-9]+)`)
	// The desustream embeds come in a few layouts: a bare <video><source src>,
	// a playerjs config (file:"…" or "file":"…"), or a Blogger iframe. Tried
	// in that order.
	sourceSrcRe  = regexp.MustCompile(`<source[^>]+src=["']([^"']+)["']`)
	playerFileRe = regexp.MustCompile(`"?file"?\s*:\s*["\']([^"\']+)["\']`)
	qualityRe    = regexp.MustCompile(`(\d{3,4})`)
	subIndoRe    = regexp.MustCompile(`(?i)\s*(?:subtitle|sub)\s+indo(?:nesia)?\s*$`)
)

// OtakudesuClient handles interactions with otakudesu.
type OtakudesuClient struct {
	client     *http.Client
	prober     *http.Client // plain net/http: file hosts (pixeldrain, googlevideo) omit ALPN and break surf's h2 path
	baseURL    string
	pixeldrain string // file host; overridden in tests so the probe stays offline
	maxRetries int
	retryDelay time.Duration
}

// NewOtakudesuClient performs no network I/O: it runs under sync.Once in the adapter.
func NewOtakudesuClient() *OtakudesuClient {
	return &OtakudesuClient{client: util.NewFastClient(), prober: newProber(), baseURL: otakudesuBase, pixeldrain: pixeldrainBase, maxRetries: 2, retryDelay: 300 * time.Millisecond}
}

// NewClientForTest points the client at a test server with retries disabled.
func NewClientForTest(serverURL string) *OtakudesuClient {
	c := NewOtakudesuClient()
	c.baseURL = strings.TrimSuffix(serverURL, "/")
	c.pixeldrain = c.baseURL
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
func (c *OtakudesuClient) fetch(ctx context.Context, rawURL, layer string, form url.Values) ([]byte, error) {
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

func (c *OtakudesuClient) document(ctx context.Context, rawURL, layer string) (*goquery.Document, error) {
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
func (c *OtakudesuClient) SearchAnime(ctx context.Context, query string) ([]*models.Anime, error) {
	// CLI args arrive hyphenated ("naruto-kecil"); WordPress reads "-kecil" as
	// an exclusion and returns nothing, so hyphens go back to spaces (as Goyabu).
	query = strings.Join(strings.FieldsFunc(query, func(r rune) bool { return r == '-' || r == '_' || r == ' ' }), " ")
	if query == "" {
		return nil, netx.NewParserError(sourceLabel, "search", "empty query", nil)
	}
	doc, err := c.document(ctx, fmt.Sprintf("%s/?s=%s&post_type=anime", c.baseURL, url.QueryEscape(query)), "search")
	if err != nil {
		return nil, err
	}
	var out []*models.Anime
	doc.Find("ul.chivsrc li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("h2 a").First()
		href, title := a.AttrOr("href", ""), cleanTitle(a.Text())
		if !strings.Contains(href, "/anime/") || title == "" {
			return
		}
		out = append(out, &models.Anime{
			Name:      title,
			URL:       href,
			ImageURL:  li.Find("img").AttrOr("src", ""),
			Source:    sourceLabel,
			MediaType: models.MediaTypeAnime,
		})
	})
	return out, nil
}

// GetAnimeEpisodes lists an anime's episodes, ascending. Batch/"lengkap" links
// (bulk download bundles) are skipped because they are not single episodes.
func (c *OtakudesuClient) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	doc, err := c.document(ctx, animeURL, "episodes")
	if err != nil {
		return nil, err
	}
	var eps []models.Episode
	doc.Find("div.episodelist ul li a[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		// "pembatas" rows are separators ("episodes 1-900 in progress"), not episodes.
		if !strings.Contains(href, "/episode/") || strings.Contains(href, "pembatas") {
			return
		}
		num := episodeNumber(href, a.Text())
		if num == 0 {
			return
		}
		eps = append(eps, models.Episode{
			Number: strconv.Itoa(num),
			Num:    num,
			URL:    href,
			Title:  models.TitleDetails{English: cleanTitle(a.Text())},
		})
	})
	if len(eps) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no episodes found (page layout changed?)", nil)
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Num < eps[j].Num })
	return eps, nil
}

// mirror is one entry of the episode page's mirror list.
type mirror struct {
	ID      int    `json:"id"`
	Index   int    `json:"i"`
	Quality string `json:"q"`
	label   string
}

// Qualities lists the resolutions the episode offers — the download section's
// Pixeldrain files (up to 1080p) and the streaming mirrors — highest first
// ("1080p", "720p", …). Whether a candidate is actually playable from here is
// only known once it is resolved, so a pick may still fall through.
func (c *OtakudesuClient) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var heights []int
	for _, d := range parseDownloads(doc) {
		if !seen[d.height] {
			seen[d.height] = true
			heights = append(heights, d.height)
		}
	}
	for _, m := range parseMirrors(doc) {
		if h := heightOf(m.Quality); h > 0 && !seen[h] {
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

// GetEpisodeStreamURL resolves an episode to a direct file or Blogger URL.
// quality is "best"/"" or a label such as "720p"; candidates of the requested
// height are tried first, then the rest by height descending.
//
// The download section's Pixeldrain files go before the streaming mirrors:
// they reach 1080p (the mirrors stop at 720p), they are plain files mpv can
// seek in, and they never carry an IP-locked URL.
func (c *OtakudesuClient) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (string, map[string]string, error) {
	body, err := c.fetch(ctx, episodeURL, "episode", nil)
	if err != nil {
		return "", nil, err
	}
	page := string(body)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return "", nil, netx.NewParserError(sourceLabel, "episode", "failed to parse episode page", err)
	}
	meta := map[string]string{"source": "otakudesu", "referer": c.baseURL + "/"}
	want := heightOf(quality)

	downloads := parseDownloads(doc)
	sort.SliceStable(downloads, func(i, j int) bool {
		hi, hj := downloads[i].height, downloads[j].height
		if want > 0 && (hi == want) != (hj == want) {
			return hi == want
		}
		return hi > hj
	})
	for _, d := range downloads {
		if u := c.resolvePixeldrain(ctx, d.url); u != "" {
			util.Debug("Otakudesu download link resolved", "label", d.label)
			return u, meta, nil
		}
	}

	mirrors := parseMirrors(doc)
	sort.SliceStable(mirrors, func(i, j int) bool {
		hi, hj := heightOf(mirrors[i].Quality), heightOf(mirrors[j].Quality)
		if want > 0 && (hi == want) != (hj == want) {
			return hi == want
		}
		return hi > hj
	})

	nonceAction, embedAction := nonceActionRe.FindStringSubmatch(page), embedActionRe.FindStringSubmatch(page)
	var nonce string
	var lastErr error
	if len(mirrors) > 0 && nonceAction != nil && embedAction != nil {
		nonce, lastErr = c.ajaxData(ctx, url.Values{"action": {nonceAction[1]}})
	}
	for _, m := range mirrors {
		if nonce == "" {
			break
		}
		src, err := c.ajaxData(ctx, url.Values{
			"id": {strconv.Itoa(m.ID)}, "i": {strconv.Itoa(m.Index)}, "q": {m.Quality},
			"nonce": {nonce}, "action": {embedAction[1]},
		})
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := base64.StdEncoding.DecodeString(src)
		if u := c.resolveEmbed(ctx, iframeSrc(string(raw))); u != "" {
			util.Debug("Otakudesu mirror resolved", "label", m.label, "quality", m.Quality)
			return u, meta, nil
		}
	}
	// Last resort: the embed the page itself loads by default.
	if u := c.resolveEmbed(ctx, iframeSrc(page)); u != "" {
		return u, meta, nil
	}
	return "", nil, netx.NewParserError(sourceLabel, "stream", "no resolvable mirror for this episode", lastErr)
}

// ajaxData posts to admin-ajax.php and returns the "data" field.
func (c *OtakudesuClient) ajaxData(ctx context.Context, form url.Values) (string, error) {
	body, err := c.fetch(ctx, c.baseURL+"/wp-admin/admin-ajax.php", "ajax", form)
	if err != nil {
		return "", err
	}
	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Data == "" {
		return "", netx.NewParserError(sourceLabel, "ajax", "unexpected admin-ajax response", err)
	}
	return payload.Data, nil
}

// resolveEmbed turns a mirror iframe into something the player can open, or
// "". A Blogger iframe is returned as is (the player unwraps those itself); a
// desustream embed is fetched and its file or Blogger URL extracted. Other
// hosts (kraken, mp4upload, filedon, vidhide, mega) are not handled.
//
// A direct file is probed before it is returned: some embeds (otakuplay /
// otakustream / ondesuhd) carry a googlevideo URL pinned to the embed server's
// IP, which answers 403 to everyone else and would make mpv fail silently.
func (c *OtakudesuClient) resolveEmbed(ctx context.Context, embedURL string) string {
	if bloggerRe.MatchString(embedURL) {
		return bloggerRe.FindString(embedURL)
	}
	if embedURL == "" || !strings.Contains(embedURL, "desustream") {
		return ""
	}
	body, err := c.fetch(ctx, embedURL, "embed", nil)
	if err != nil {
		return ""
	}
	for _, re := range []*regexp.Regexp{sourceSrcRe, playerFileRe} {
		if m := re.FindSubmatch(body); m != nil && strings.HasPrefix(string(m[1]), "http") {
			if u := html.UnescapeString(string(m[1])); c.playable(ctx, u) {
				return u
			}
			util.Debug("Otakudesu embed file not playable from here; trying next mirror", "embed", embedURL)
			return ""
		}
	}
	return string(bloggerRe.Find(body))
}

// playable asks for the first byte of a file URL and reports whether the host
// serves it (200/206). Cheap enough to run once per candidate mirror.
func (c *OtakudesuClient) playable(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- URL comes from the pinned host's embed
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// download is one Pixeldrain entry of the episode page's download section:
// "<strong>MKV 1080p</strong> <a>Filedon</a> <a>Pdrain</a> …".
type download struct {
	label  string
	height int
	url    string // link.desustream.com redirector
}

// parseDownloads keeps the Pdrain link of every row that has one. Other hosts
// (Filedon, Acefile, GoFile, Mega, KFiles) need per-host scraping.
func parseDownloads(doc *goquery.Document) []download {
	var out []download
	doc.Find("div.download li").Each(func(_ int, li *goquery.Selection) {
		label := strings.TrimSpace(li.Find("strong").First().Text())
		h := heightOf(label)
		if h == 0 {
			return
		}
		li.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			name := strings.ToLower(strings.TrimSpace(a.Text()))
			if strings.Contains(name, "pdrain") || strings.Contains(name, "pixeldrain") {
				out = append(out, download{label: label, height: h, url: a.AttrOr("href", "")})
			}
		})
	})
	return out
}

// resolvePixeldrain follows the redirector one hop, maps the Pixeldrain page
// (pixeldrain.com/u/<id>) to its direct file, and probes it. Returns "" when
// the link does not land on Pixeldrain or the file is gone.
func (c *OtakudesuClient) resolvePixeldrain(ctx context.Context, redirectURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirectURL, http.NoBody)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Referer", c.baseURL+"/")
	noFollow := *c.prober
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req) // #nosec G704 -- URL comes from the pinned host's page
	if err != nil {
		return ""
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, c.pixeldrain+"/") {
		return "" // Mega, GoFile, … — not a host this client can stream from
	}
	m := pixeldrainIDRe.FindStringSubmatch(strings.TrimPrefix(loc, c.pixeldrain))
	if m == nil {
		return ""
	}
	u := c.pixeldrain + "/api/file/" + m[1]
	if !c.playable(ctx, u) {
		return ""
	}
	return u
}

func parseMirrors(doc *goquery.Document) []mirror {
	var out []mirror
	doc.Find("div.mirrorstream a[data-content]").Each(func(_ int, a *goquery.Selection) {
		raw, err := base64.StdEncoding.DecodeString(a.AttrOr("data-content", ""))
		if err != nil {
			return
		}
		var m mirror
		if json.Unmarshal(raw, &m) == nil && m.ID != 0 {
			m.label = strings.TrimSpace(a.Text())
			out = append(out, m)
		}
	})
	return out
}

func iframeSrc(html string) string {
	if m := iframeSrcRe.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

// heightOf turns "720p"/"720" into 720; "best"/"" → 0.
func heightOf(q string) int {
	if m := qualityRe.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func episodeNumber(href, text string) int {
	for _, s := range []string{href, text} {
		if m := episodeNumRe.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}

// cleanTitle drops the site's "Subtitle Indonesia" suffix; tagging adds its own.
func cleanTitle(s string) string {
	return strings.TrimSpace(subIndoRe.ReplaceAllString(strings.TrimSpace(s), ""))
}
