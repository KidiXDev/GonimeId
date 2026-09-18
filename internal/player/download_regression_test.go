package player

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KidiXDev/GonimeId/internal/scraper/netx"
	"github.com/KidiXDev/GonimeId/internal/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDownloadDirectHTTPWithClientDownloadsMockVideoAndTracksProgress(t *testing.T) {
	payload := bytes.Repeat([]byte("gonimeid-video-payload"), 32*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/episode.mp4" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	outPath := filepath.Join(home, "downloads", "episode.mp4")
	m := &model{}

	err := downloadDirectHTTPWithClient(server.URL+"/episode.mp4", outPath, m, server.Client())
	require.NoError(t, err)

	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, int64(len(payload)), m.progressTotal())

	m.mu.Lock()
	received := m.received
	m.mu.Unlock()
	assert.Equal(t, int64(len(payload)), received)
}

func TestDownloadPartStopsAfterRepeatedRequestErrors(t *testing.T) {
	restore := setDownloadPartRetryDelayForTest(0)
	defer restore()

	var attempts int
	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			attempts++
			return nil, errors.New("temporary network failure")
		}),
	}

	err := downloadPart(
		"https://allanime.day/video/episode.mp4",
		0,
		6,
		0,
		client,
		filepath.Join(t.TempDir(), "episode.mp4"),
		&model{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries (20) exceeded")
	assert.Equal(t, 20, attempts)
}

func TestDownloadPartStopsAfterRepeatedHTTPStatusWithoutProgress(t *testing.T) {
	restore := setDownloadPartRetryDelayForTest(0)
	defer restore()

	var attempts int
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("source unavailable")),
				Request:    req,
			}, nil
		}),
	}

	err := downloadPart(
		"https://allanime.day/video/episode.mp4",
		0,
		6,
		0,
		client,
		filepath.Join(t.TempDir(), "episode.mp4"),
		&model{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries (20) exceeded")
	assert.Equal(t, 20, attempts)
}

func setDownloadPartRetryDelayForTest(delay time.Duration) func() {
	original := downloadPartRetryDelay
	downloadPartRetryDelay = delay
	return func() {
		downloadPartRetryDelay = original
	}
}

func snapshotGlobalReferer() func() {
	referer := util.GetGlobalReferer()
	return func() {
		if referer == "" {
			util.ClearGlobalReferer()
			return
		}
		util.SetGlobalReferer(referer)
	}
}

func TestDownloadDirectHTTPWithClientReturnsHTTPStatusErrorFromMockCDN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing object", http.StatusNotFound)
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	outPath := filepath.Join(home, "downloads", "episode.mp4")

	err := downloadDirectHTTPWithClient(server.URL+"/missing.mp4", outPath, &model{}, server.Client())
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")
	diagnostic := netx.DiagnoseError("Download", "http", err)
	require.NotNil(t, diagnostic)
	assert.Equal(t, netx.DiagnosticDownloadExpired, diagnostic.Kind)

	_, statErr := os.Stat(outPath)
	assert.True(t, os.IsNotExist(statErr), "404 response must not create a completed file")
}
