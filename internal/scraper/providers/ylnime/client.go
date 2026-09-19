package ylnime

import (
	"context"
	"encoding/json"
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
	ylnimeBase   = "https://ylnime.com"
	sourceLabel  = "YLnime"
	maxBodyBytes = 8 << 20
)

var (
	episodeNumRe = regexp.MustCompile(`(?i)episode\s*(\d+)`)
	qualityRe    = regexp.MustCompile(`(?i)(\d{3,4})p`)
	streamsRe    = regexp.MustCompile(`(?s)\bconst\s+streams\s*=\s*(\[[^;]*\])\s*;`)
)

type stream struct {
	Resolution string `json:"reso"`
	Link       string `json:"link"`
}

// Client handles ylnime.com. Construction performs no network I/O.
type Client struct {
	client     *http.Client
	prober     *http.Client
	baseURL    string
	maxRetries int
	retryDelay time.Duration
}

func NewYlnimeClient() *Client {
	return &Client{
		client:     util.NewFastClient(),
		prober:     &http.Client{Transport: netx.SafeScraperTransport(8 * time.Second), Timeout: 10 * time.Second},
		baseURL:    ylnimeBase,
		maxRetries: 2,
		retryDelay: 300 * time.Millisecond,
	}
}

func NewClientForTest(serverURL string) *Client {
	c := NewYlnimeClient()
	c.baseURL = strings.TrimSuffix(serverURL, "/")
	c.maxRetries, c.retryDelay = 0, 0
	c.prober = &http.Client{Timeout: 5 * time.Second}
	return c
}

func (c *Client) fetch(ctx context.Context, rawURL, layer string) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
		if err != nil {
			return nil, netx.NewParserError(sourceLabel, layer, "bad request URL", err)
		}
		req.Header.Set("User-Agent", netx.UserAgent)
		req.Header.Set("Referer", c.baseURL+"/")
		resp, err := c.client.Do(req) // #nosec G704 -- URLs are the pinned site or links it supplied
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.retryDelay):
		}
	}
}

func (c *Client) document(ctx context.Context, rawURL, layer string) (*goquery.Document, error) {
	body, err := c.fetch(ctx, rawURL, layer)
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

func (c *Client) absolute(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	base, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func (c *Client) SearchAnime(ctx context.Context, query string) ([]*models.Anime, error) {
	query = strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(query, "-", " "), "_", " ")), " ")
	if query == "" {
		return nil, netx.NewParserError(sourceLabel, "search", "empty query", nil)
	}
	doc, err := c.document(ctx, c.baseURL+"/?search="+url.QueryEscape(query), "search")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []*models.Anime
	doc.Find(`a[href*="series="]`).Each(func(_ int, a *goquery.Selection) {
		card := a.Closest(".card")
		title, href := strings.TrimSpace(card.Find(".card-title").First().Text()), c.absolute(a.AttrOr("href", ""))
		if title == "" || href == "" || seen[href] {
			return
		}
		seen[href] = true
		out = append(out, &models.Anime{Name: title, URL: href, ImageURL: c.absolute(card.Find("img").First().AttrOr("src", "")), Source: sourceLabel, MediaType: models.MediaTypeAnime})
	})
	return out, nil
}

func (c *Client) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	doc, err := c.document(ctx, animeURL, "episodes")
	if err != nil {
		return nil, err
	}
	var eps []models.Episode
	doc.Find(`a.list-group-item[href*="episode="]`).Each(func(_ int, a *goquery.Selection) {
		label := strings.TrimSpace(a.Find("span").First().Text())
		m := episodeNumRe.FindStringSubmatch(label)
		if m == nil {
			return
		}
		num, _ := strconv.Atoi(m[1])
		eps = append(eps, models.Episode{Number: m[1], Num: num, URL: c.absolute(a.AttrOr("href", "")), Title: models.TitleDetails{English: label}})
	})
	if len(eps) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no episodes found (page layout changed?)", nil)
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Num < eps[j].Num })
	return eps, nil
}

func (c *Client) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var heights []int
	doc.Find(`a[href*="reso="]`).Each(func(_ int, a *goquery.Selection) {
		m := qualityRe.FindStringSubmatch(a.Text())
		if m == nil {
			return
		}
		h, _ := strconv.Atoi(m[1])
		if h > 0 && !seen[h] {
			seen[h] = true
			heights = append(heights, h)
		}
	})
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	out := make([]string, len(heights))
	for i, h := range heights {
		out[i] = strconv.Itoa(h) + "p"
	}
	return out, nil
}

func (c *Client) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (streamURL string, metadata map[string]string, err error) {
	u, err := url.Parse(episodeURL)
	if err != nil {
		return "", nil, netx.NewParserError(sourceLabel, "stream", "bad episode URL", err)
	}
	if quality != "" && quality != "best" {
		q := u.Query()
		q.Set("reso", quality)
		u.RawQuery = q.Encode()
	}
	body, err := c.fetch(ctx, u.String(), "episode")
	if err != nil {
		return "", nil, err
	}
	streams, err := parseStreams(body)
	if err != nil {
		return "", nil, err
	}
	for _, candidate := range streams {
		if c.playable(ctx, candidate.Link) {
			util.Debug("YLnime stream resolved", "quality", candidate.Resolution)
			return candidate.Link, map[string]string{"source": "ylnime", "referer": c.baseURL + "/"}, nil
		}
	}
	return "", nil, netx.NewParserError(sourceLabel, "stream", "no playable server for this episode", nil)
}

func parseStreams(body []byte) ([]stream, error) {
	m := streamsRe.FindSubmatch(body)
	if m == nil {
		return nil, netx.NewParserError(sourceLabel, "stream", "no player streams on episode page (layout changed?)", nil)
	}
	var streams []stream
	if err := json.Unmarshal(m[1], &streams); err != nil {
		return nil, netx.NewParserError(sourceLabel, "stream", "invalid player stream data", err)
	}
	valid := streams[:0]
	for _, candidate := range streams {
		u, err := url.Parse(candidate.Link)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			valid = append(valid, candidate)
		}
	}
	if len(valid) == 0 {
		return nil, netx.NewParserError(sourceLabel, "stream", "player stream list is empty", nil)
	}
	return valid, nil
}

func (c *Client) playable(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Referer", c.baseURL+"/")
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- SafeScraperTransport blocks private destinations
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}
