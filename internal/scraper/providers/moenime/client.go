package moenime

import (
	"context"
	"fmt"
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
	moenimeBase  = "https://moenime.com"
	moeclipBase  = "https://moeclip.com"
	sourceLabel  = "Moenime"
	maxBodyBytes = 8 << 20
)

var (
	episodePathRe = regexp.MustCompile(`/([0-9]+?)/?$`)
	qualityRe     = regexp.MustCompile(`(?i)([0-9]{3,4})p`)
	playerFileRe  = regexp.MustCompile(`\bfile\s*:\s*['"]([^'"]+)['"]`)
	titleNoiseRe  = regexp.MustCompile(`(?i)\s*(?:\(episode\s*\d+\)\s*)?sub\s+indo\s*$`)
)

// Client handles Moenime listings and their Moeclip players.
type Client struct {
	client     *http.Client
	prober     *http.Client
	baseURL    string
	maxRetries int
	retryDelay time.Duration
}

func NewMoenimeClient() *Client {
	return &Client{
		client:     util.NewFastClient(),
		prober:     &http.Client{Transport: netx.SafeScraperTransport(8 * time.Second), Timeout: 10 * time.Second},
		baseURL:    moenimeBase,
		maxRetries: 2,
		retryDelay: 300 * time.Millisecond,
	}
}

func NewClientForTest(serverURL string) *Client {
	c := NewMoenimeClient()
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
		resp, err := c.client.Do(req) // #nosec G704 -- URLs come from the pinned sites' pages
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
	query = strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(query)), " ")
	if query == "" {
		return nil, netx.NewParserError(sourceLabel, "search", "empty query", nil)
	}
	doc, err := c.document(ctx, c.baseURL+"/?s="+url.QueryEscape(query), "search")
	if err != nil {
		return nil, err
	}
	var out []*models.Anime
	doc.Find("article").Each(func(_ int, article *goquery.Selection) {
		a := article.Find("h1.entry-title a").First()
		title, href := cleanTitle(a.Text()), c.absolute(a.AttrOr("href", ""))
		if title == "" || href == "" {
			return
		}
		out = append(out, &models.Anime{
			Name: title, URL: href,
			ImageURL: c.absolute(article.Find(".featured-thumb img").First().AttrOr("src", "")),
			Source:   sourceLabel, MediaType: models.MediaTypeAnime,
		})
	})
	return out, nil
}

func (c *Client) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	doc, err := c.document(ctx, animeURL, "episodes")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var episodes []models.Episode
	doc.Find("a.moe-stream-a").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		rowID := strings.TrimSpace(a.Closest("td[id]").AttrOr("id", ""))
		if rowID == "" {
			if m := episodePathRe.FindStringSubmatch(href); m != nil {
				rowID = m[1]
			}
		}
		num, err := strconv.Atoi(rowID)
		if err != nil || num < 1 || href == "" || seen[href] {
			return
		}
		seen[href] = true
		episodes = append(episodes, models.Episode{
			Number: strconv.Itoa(num), Num: num, URL: href,
			Title: models.TitleDetails{English: fmt.Sprintf("Episode %d", num)},
		})
	})
	if len(episodes) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no Moeclip episodes found (page layout changed?)", nil)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i].Num < episodes[j].Num })
	return episodes, nil
}

type playerInfo struct {
	mediaID, postID, origin string
	qualities               []string
}

func (c *Client) playerInfo(ctx context.Context, episodeURL string) (playerInfo, error) {
	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return playerInfo{}, err
	}
	info := playerInfo{
		mediaID: strings.TrimSpace(doc.Find("button.mirrorlist.aktif").First().AttrOr("meta-src", "")),
		postID:  strings.TrimSpace(doc.Find("#down-title").First().Text()),
	}
	u, err := url.Parse(episodeURL)
	if err == nil && u.Scheme != "" && u.Host != "" {
		info.origin = u.Scheme + "://" + u.Host
	}
	seen := map[int]bool{}
	var heights []int
	doc.Find("#source option").Each(func(_ int, option *goquery.Selection) {
		m := qualityRe.FindStringSubmatch(option.AttrOr("value", option.Text()))
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
	for _, h := range heights {
		info.qualities = append(info.qualities, strconv.Itoa(h)+"p")
	}
	if info.mediaID == "" || info.postID == "" || info.origin == "" || len(info.qualities) == 0 {
		return playerInfo{}, netx.NewParserError(sourceLabel, "episode", "incomplete Moeclip player data (layout changed?)", nil)
	}
	return info, nil
}

func (c *Client) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	info, err := c.playerInfo(ctx, episodeURL)
	return info.qualities, err
}

func (c *Client) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (string, map[string]string, error) {
	info, err := c.playerInfo(ctx, episodeURL)
	if err != nil {
		return "", nil, err
	}
	selected := info.qualities[0]
	for _, offered := range info.qualities {
		if strings.EqualFold(offered, quality) {
			selected = offered
			break
		}
	}
	playerURL := fmt.Sprintf("%s/v/%s_%s_%s.html", info.origin, info.mediaID, selected, info.postID)
	body, err := c.fetch(ctx, playerURL, "player")
	if err != nil {
		return "", nil, err
	}
	m := playerFileRe.FindSubmatch(body)
	if m == nil {
		return "", nil, netx.NewParserError(sourceLabel, "player", "no media file in Moeclip player (layout changed?)", nil)
	}
	streamURL := string(m[1])
	if !c.playable(ctx, streamURL) {
		return "", nil, netx.NewParserError(sourceLabel, "stream", "Moeclip media file is not playable", nil)
	}
	util.Debug("Moenime stream resolved", "quality", selected)
	return streamURL, map[string]string{"source": "moenime", "referer": info.origin + "/"}, nil
}

func (c *Client) playable(ctx context.Context, rawURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Referer", moeclipBase+"/")
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- SafeScraperTransport blocks private destinations
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

func cleanTitle(title string) string {
	return strings.TrimSpace(titleNoiseRe.ReplaceAllString(strings.TrimSpace(title), ""))
}
