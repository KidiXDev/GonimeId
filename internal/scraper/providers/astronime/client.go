package astronime

import (
	"bytes"
	"context"
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
	astronimeBase = "https://astronime.id"
	abyssDecrypt  = "https://enc-dec.app/api/dec-abyss"
	sourceLabel   = "Astronime"
	maxBodyBytes  = 8 << 20
)

var (
	episodeNumRe = regexp.MustCompile(`(?i)episode[-\s]+(\d+)`)
	qualityRe    = regexp.MustCompile(`(?i)(\d{3,4})p?`)
	abyssDataRe  = regexp.MustCompile(`const\s+datas\s*=\s*"([^"]+)"`)
)

type streamSource struct {
	File  string
	Label string
}

type abyssResponse struct {
	Status int `json:"status"`
	Result struct {
		Sources []struct {
			URL    string `json:"url"`
			Type   string `json:"type"`
			Status bool   `json:"status"`
		} `json:"sources"`
	} `json:"result"`
}

type cachedEpisode struct {
	sources []streamSource
	referer string
	at      time.Time
}

// Client handles Astronime listings and their Hydrax players.
type Client struct {
	client     *http.Client
	prober     *http.Client
	baseURL    string
	decryptURL string
	maxRetries int
	retryDelay time.Duration
	cacheMu    sync.Mutex
	cache      map[string]cachedEpisode
}

func NewAstronimeClient() *Client {
	return &Client{
		client:     util.NewFastClient(),
		prober:     &http.Client{Transport: netx.SafeScraperTransport(8 * time.Second), Timeout: 10 * time.Second},
		baseURL:    astronimeBase,
		decryptURL: abyssDecrypt,
		maxRetries: 2,
		retryDelay: 300 * time.Millisecond,
		cache:      make(map[string]cachedEpisode),
	}
}

func NewClientForTest(serverURL string) *Client {
	c := NewAstronimeClient()
	c.baseURL = strings.TrimSuffix(serverURL, "/")
	c.decryptURL = c.baseURL + "/decrypt"
	c.maxRetries, c.retryDelay = 0, 0
	c.prober = &http.Client{Timeout: 5 * time.Second}
	return c
}

func (c *Client) request(ctx context.Context, method, rawURL, contentType, referer, origin, layer string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
		if err != nil {
			return nil, netx.NewParserError(sourceLabel, layer, "bad request URL", err)
		}
		req.Header.Set("User-Agent", netx.UserAgent)
		if referer == "" {
			referer = c.baseURL + "/"
		}
		req.Header.Set("Referer", referer)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := c.client.Do(req) // #nosec G704 -- URLs come from the pinned sites' pages
		if err == nil {
			responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
			_ = resp.Body.Close()
			switch {
			case readErr != nil:
				err = readErr
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				return responseBody, nil
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
	body, err := c.request(ctx, http.MethodGet, rawURL, "", "", "", layer, nil)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
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
	var anime []*models.Anime
	doc.Find("article.animpost").Each(func(_ int, card *goquery.Selection) {
		a := card.Find(".animposx > a, a[title]").First()
		title, href := strings.TrimSpace(a.AttrOr("title", a.Text())), c.absolute(a.AttrOr("href", ""))
		if title == "" || href == "" {
			return
		}
		img := card.Find("img").First()
		imageURL := img.AttrOr("data-src", img.AttrOr("src", ""))
		anime = append(anime, &models.Anime{Name: title, URL: href, ImageURL: c.absolute(imageURL), Source: sourceLabel, MediaType: models.MediaTypeAnime})
	})
	return anime, nil
}

func (c *Client) GetAnimeEpisodes(ctx context.Context, animeURL string) ([]models.Episode, error) {
	doc, err := c.document(ctx, animeURL, "episodes")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var episodes []models.Episode
	doc.Find(`a[href*="-episode-"]`).Each(func(_ int, a *goquery.Selection) {
		href := c.absolute(a.AttrOr("href", ""))
		match := episodeNumRe.FindStringSubmatch(href + " " + a.Text())
		if href == "" || match == nil || seen[href] {
			return
		}
		num, err := strconv.Atoi(match[1])
		if err != nil || num < 1 {
			return
		}
		seen[href] = true
		episodes = append(episodes, models.Episode{Number: strconv.Itoa(num), Num: num, URL: href, Title: models.TitleDetails{English: fmt.Sprintf("Episode %d", num)}})
	})
	if len(episodes) == 0 {
		return nil, netx.NewParserError(sourceLabel, "episodes", "no episodes found (page layout changed?)", nil)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i].Num < episodes[j].Num })
	return episodes, nil
}

func (c *Client) Qualities(ctx context.Context, episodeURL string) ([]string, error) {
	sources, _, err := c.episodeSources(ctx, episodeURL)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool)
	var heights []int
	for _, source := range sources {
		height := qualityHeight(source.Label)
		if height > 0 && !seen[height] {
			seen[height] = true
			heights = append(heights, height)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	qualities := make([]string, 0, len(heights))
	for _, height := range heights {
		qualities = append(qualities, strconv.Itoa(height)+"p")
	}
	return qualities, nil
}

func (c *Client) GetEpisodeStreamURL(ctx context.Context, episodeURL, quality string) (streamURL string, metadata map[string]string, err error) {
	sources, referer, err := c.episodeSources(ctx, episodeURL)
	if err != nil {
		return "", nil, err
	}
	sort.SliceStable(sources, func(i, j int) bool { return qualityHeight(sources[i].Label) > qualityHeight(sources[j].Label) })
	wanted := qualityHeight(quality)
	if wanted > 0 {
		for i, source := range sources {
			if qualityHeight(source.Label) == wanted {
				sources[0], sources[i] = sources[i], sources[0]
				break
			}
		}
	}
	for _, source := range sources {
		if c.playable(ctx, source.File, referer) {
			util.Debug("Astronime stream resolved", "quality", source.Label)
			return source.File, map[string]string{"source": "astronime", "referer": referer}, nil
		}
	}
	return "", nil, netx.NewParserError(sourceLabel, "stream", "no playable Hydrax media file", nil)
}

func (c *Client) episodeSources(ctx context.Context, episodeURL string) ([]streamSource, string, error) {
	c.cacheMu.Lock()
	cached, ok := c.cache[episodeURL]
	c.cacheMu.Unlock()
	if ok && time.Since(cached.at) < 2*time.Minute {
		return append([]streamSource(nil), cached.sources...), cached.referer, nil
	}

	doc, err := c.document(ctx, episodeURL, "episode")
	if err != nil {
		return nil, "", err
	}
	var postID, serverID string
	doc.Find(".east_player_option").EachWithBreak(func(_ int, option *goquery.Selection) bool {
		if !strings.Contains(strings.ToLower(option.Text()), "hydrax") {
			return true
		}
		postID = strings.TrimSpace(option.AttrOr("data-post", ""))
		serverID = strings.TrimSpace(option.AttrOr("data-nume", ""))
		return false
	})
	if postID == "" || serverID == "" {
		return nil, "", netx.NewParserError(sourceLabel, "episode", "Hydrax server not found (page layout changed?)", nil)
	}
	form := url.Values{"action": {"player_ajax"}, "post": {postID}, "nume": {serverID}, "type": {"urliframe"}}
	body, err := c.request(ctx, http.MethodPost, c.baseURL+"/wp-admin/admin-ajax.php", "application/x-www-form-urlencoded; charset=UTF-8", episodeURL, c.baseURL, "player", []byte(form.Encode()))
	if err != nil {
		return nil, "", err
	}
	player, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, "", netx.NewParserError(sourceLabel, "player", "failed to parse player response", err)
	}
	embedURL := c.absolute(player.Find("iframe[src]").First().AttrOr("src", ""))
	if embedURL == "" {
		return nil, "", netx.NewParserError(sourceLabel, "player", "Hydrax iframe not found", nil)
	}
	embed, err := c.request(ctx, http.MethodGet, embedURL, "", episodeURL, "", "Hydrax embed", nil)
	if err != nil {
		return nil, "", err
	}
	match := abyssDataRe.FindSubmatch(embed)
	if match == nil {
		return nil, "", netx.NewParserError(sourceLabel, "Hydrax embed", "encrypted player data not found", nil)
	}
	payload, _ := json.Marshal(map[string]string{"text": string(match[1])})
	decoded, err := c.request(ctx, http.MethodPost, c.decryptURL, "application/json", "https://playhydrax.com/", "https://playhydrax.com", "Hydrax decode", payload)
	if err != nil {
		return nil, "", err
	}
	var response abyssResponse
	if err := json.Unmarshal(decoded, &response); err != nil {
		return nil, "", netx.NewParserError(sourceLabel, "Hydrax decode", "invalid decoder response", err)
	}
	if response.Status != http.StatusOK {
		return nil, "", netx.NewParserError(sourceLabel, "Hydrax decode", "decoder rejected player data", nil)
	}
	var sources []streamSource
	for _, source := range response.Result.Sources {
		file, err := netx.ValidateStreamURL(source.URL, sourceLabel)
		if !source.Status || err != nil || qualityHeight(source.Type) == 0 {
			continue
		}
		sources = append(sources, streamSource{File: file, Label: source.Type})
	}
	if len(sources) == 0 {
		return nil, "", netx.NewParserError(sourceLabel, "Hydrax decode", "no media sources found", nil)
	}
	c.cacheMu.Lock()
	c.cache[episodeURL] = cachedEpisode{sources: append([]streamSource(nil), sources...), referer: embedURL, at: time.Now()}
	c.cacheMu.Unlock()
	return sources, embedURL, nil
}

func qualityHeight(label string) int {
	match := qualityRe.FindStringSubmatch(label)
	if match == nil {
		return 0
	}
	height, _ := strconv.Atoi(match[1])
	return height
}

func (c *Client) playable(ctx context.Context, rawURL, referer string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", netx.UserAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.prober.Do(req) // #nosec G704 -- SafeScraperTransport blocks private destinations
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}
