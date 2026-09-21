// tracking/sqlite_tracker.go
package tracking

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// IsCgoEnabled indicates whether CGO is enabled for SQLite support
var IsCgoEnabled = true

// Error constants
var (
	ErrCgoDisabled      = errors.New("CGO disabled: sqlite tracking not available")
	ErrTrackerNotInited = errors.New("tracker not initialized")
)

/*
────────────────────────────────────────────────────────────────────────────*
│  Constantes de Configuração                                                │
*────────────────────────────────────────────────────────────────────────────
*/
const (
	defaultCacheSize  = -32000    // 32MB (increased from 20MB for better performance)
	mmapSize          = 536870912 // 512MB (increased from 256MB)
	busyTimeout       = 3000      // 3 seconds (reduced from 5s for faster response)
	walAutoCheckpoint = 500       // pages (reduced for more frequent checkpoints)
	maxOpenConns      = 3         // reduced for lower overhead
	maxIdleConns      = 2         // keep idle connections
	avgAnimePerUser   = 100       // pre-allocation for slices
)

/*
────────────────────────────────────────────────────────────────────────────*
│  Tipos e Estruturas                                                        │
*────────────────────────────────────────────────────────────────────────────
*/

// Anime represents tracked media (anime, movie, or TV show)
type Anime struct {
	AnilistID     int       `json:"anilist_id"`
	AllanimeID    string    `json:"allanime_id"` // Primary key - unique per content
	SeriesKey     string    `json:"series_key"`
	SeriesURL     string    `json:"series_url"`
	SeriesTitle   string    `json:"series_title"`
	Source        string    `json:"source"`
	EpisodeURL    string    `json:"episode_url"`
	TotalEpisodes int       `json:"total_episodes"`
	EpisodeNumber int       `json:"episode_number"`
	PlaybackTime  int       `json:"playback_time"`
	Duration      int       `json:"duration"`
	Title         string    `json:"title"`
	MediaType     string    `json:"media_type"` // "anime" or "movie"
	Completed     bool      `json:"completed"`
	LastUpdated   time.Time `json:"last_updated"`
}

// ProgressPercent returns a clamped integer percentage for display.
func (a Anime) ProgressPercent() int {
	if a.Duration <= 0 {
		return 0
	}
	return min(max(a.PlaybackTime*100/a.Duration, 0), 100)
}

// Series groups episode progress for one title in recent-activity order.
type Series struct {
	Key             string
	URL             string
	Title           string
	Source          string
	MediaType       string
	AnilistID       int
	Episodes        []Anime
	ContinueEpisode int
	TotalEpisodes   int
	LastUpdated     time.Time
}

// Finished reports whether every known episode has an explicit completion.
func (s Series) Finished() bool {
	if s.TotalEpisodes <= 0 {
		return false
	}
	completed := make(map[int]struct{}, s.TotalEpisodes)
	for _, episode := range s.Episodes {
		if episode.Completed {
			completed[episode.EpisodeNumber] = struct{}{}
		}
	}
	return len(completed) >= s.TotalEpisodes
}

type LocalTracker struct {
	db       *sql.DB
	upsertPS *sql.Stmt
	getPS    *sql.Stmt
	allPS    *sql.Stmt
	deletePS *sql.Stmt
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Singleton/Cache Global do Tracker                                         │
*────────────────────────────────────────────────────────────────────────────
*/
var (
	globalTracker     *LocalTracker
	globalTrackerPath string
	trackerMutex      = &sync.Mutex{}
)

// GetGlobalTracker returns the cached global tracker instance.
// This avoids repeatedly opening the database connection which is slow.
func GetGlobalTracker() *LocalTracker {
	trackerMutex.Lock()
	defer trackerMutex.Unlock()
	return globalTracker
}

// CloseGlobalTracker closes the global tracker and clears the cache.
// Should be called on application shutdown.
func CloseGlobalTracker() error {
	trackerMutex.Lock()
	defer trackerMutex.Unlock()

	if globalTracker != nil {
		err := globalTracker.Close()
		globalTracker = nil
		globalTrackerPath = ""
		return err
	}
	return nil
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Construtor e Inicialização                                                │
*────────────────────────────────────────────────────────────────────────────
*/
var NewLocalTracker func(dbPath string) *LocalTracker

func newLocalTrackerImpl(dbPath string) *LocalTracker {
	// Use singleton pattern to avoid repeatedly opening the database
	trackerMutex.Lock()
	defer trackerMutex.Unlock()

	// Return cached tracker if path matches
	if globalTracker != nil && globalTrackerPath == dbPath {
		return globalTracker
	}
	// Check if CGO is disabled (SQLite not available)
	if !IsCgoEnabled {
		fmt.Println("Warning: CGO is disabled, anime progress tracking will be unavailable")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		fmt.Printf("Error creating data directory: %v\n", err)
		return nil
	}
	// Build DSN with optimized pragmas for faster operations
	var dsn string
	if runtime.GOOS == "windows" {
		// Use URI format with escape for Windows
		escapedPath := strings.ReplaceAll(dbPath, "\\", "/")
		dsn = fmt.Sprintf(
			"file:%s?_journal_mode=WAL&_synchronous=NORMAL&_wal_autocheckpoint=%d&"+
				"_busy_timeout=%d&_cache_size=%d&_mmap_size=%d&_mode=rwc&_temp_store=MEMORY",
			escapedPath,
			walAutoCheckpoint,
			busyTimeout,
			defaultCacheSize,
			mmapSize,
		)
	} else {
		dsn = fmt.Sprintf(
			"file:%s?_journal_mode=WAL&_synchronous=NORMAL&_wal_autocheckpoint=%d&"+
				"_busy_timeout=%d&_cache_size=%d&_mmap_size=%d&_temp_store=MEMORY",
			dbPath,
			walAutoCheckpoint,
			busyTimeout,
			defaultCacheSize,
			mmapSize,
		)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		return nil
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(0) // Keep connections alive indefinitely

	if err := initializeDatabase(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Printf("Error closing database: %v\n", closeErr)
		}
		fmt.Printf("Error initializing database: %v\n", err)
		return nil
	}

	statements, err := prepareStatements(db)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Printf("Error closing database: %v\n", closeErr)
		}
		fmt.Printf("Error preparing statements: %v\n", err)
		return nil
	}

	tracker := &LocalTracker{
		db:       db,
		upsertPS: statements.upsert,
		getPS:    statements.get,
		allPS:    statements.all,
		deletePS: statements.delete,
	}

	// Cache the tracker globally for reuse
	globalTracker = tracker
	globalTrackerPath = dbPath

	return tracker
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Inicialização do Banco de Dados                                           │
*────────────────────────────────────────────────────────────────────────────
*/
func initializeDatabase(db *sql.DB) error {
	schema := `CREATE TABLE IF NOT EXISTS media_progress (
		allanime_id    TEXT    PRIMARY KEY NOT NULL,
		anilist_id     INTEGER DEFAULT 0,
		series_key     TEXT    NOT NULL DEFAULT '',
		series_url     TEXT    NOT NULL DEFAULT '',
		series_title   TEXT    NOT NULL DEFAULT '',
		source         TEXT    NOT NULL DEFAULT '',
		episode_url    TEXT    NOT NULL DEFAULT '',
		total_episodes INTEGER NOT NULL DEFAULT 0,
		episode_number INTEGER NOT NULL,
		playback_time  INTEGER NOT NULL CHECK(playback_time >= 0),
		duration       INTEGER NOT NULL CHECK(duration >= 0),
		title          TEXT,
		media_type     TEXT    DEFAULT 'anime',
		completed      INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0, 1)),
		last_updated   INTEGER NOT NULL
	);`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("schema creation failed: %w", err)
	}

	// Migrate old data if anime_progress table exists
	migrateOldData(db)
	if err := migrateMediaProgress(db); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY NOT NULL,
		value TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("settings schema creation failed: %w", err)
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_lookup 
		ON media_progress(allanime_id, last_updated DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_media_type 
		ON media_progress(media_type, last_updated DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_series_progress
		ON media_progress(series_key, episode_number)`,
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("index creation '%s' failed: %w", idx, err)
		}
	}

	if _, err := db.Exec(`PRAGMA optimize`); err != nil {
		return fmt.Errorf("initial optimization failed: %w", err)
	}

	return nil
}

// migrateMediaProgress upgrades pre-watch-hub databases without discarding
// resume positions. Rows without series metadata remain usable for resume and
// become visible in the hub after that title is played again.
func migrateMediaProgress(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(media_progress)`)
	if err != nil {
		return fmt.Errorf("inspect progress schema: %w", err)
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan progress schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close progress schema rows: %w", err)
	}

	baseRequired := []string{"series_key", "series_url", "series_title", "source", "episode_url", "completed"}
	complete := true
	for _, name := range baseRequired {
		complete = complete && columns[name]
	}
	if complete && !columns["total_episodes"] {
		if _, err := db.Exec(`ALTER TABLE media_progress ADD COLUMN total_episodes INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add total episode count: %w", err)
		}
		_, err = db.Exec(`PRAGMA user_version = 2`)
		return err
	}
	complete = complete && columns["total_episodes"]
	if complete {
		_, err = db.Exec(`PRAGMA user_version = 2`)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin progress migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.Exec(`CREATE TABLE media_progress_v2 (
		allanime_id TEXT PRIMARY KEY NOT NULL,
		anilist_id INTEGER DEFAULT 0,
		series_key TEXT NOT NULL DEFAULT '',
		series_url TEXT NOT NULL DEFAULT '',
		series_title TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		episode_url TEXT NOT NULL DEFAULT '',
		total_episodes INTEGER NOT NULL DEFAULT 0,
		episode_number INTEGER NOT NULL,
		playback_time INTEGER NOT NULL CHECK(playback_time >= 0),
		duration INTEGER NOT NULL CHECK(duration >= 0),
		title TEXT,
		media_type TEXT DEFAULT 'anime',
		completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0, 1)),
		last_updated INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create upgraded progress table: %w", err)
	}
	if _, err = tx.Exec(`INSERT INTO media_progress_v2 (
		allanime_id, anilist_id, episode_number, playback_time, duration,
		title, media_type, completed, last_updated
	) SELECT allanime_id, anilist_id, episode_number, playback_time, duration,
		title, media_type,
		CASE WHEN duration > 0 AND playback_time * 100 >= duration * 90 THEN 1 ELSE 0 END,
		last_updated FROM media_progress`); err != nil {
		return fmt.Errorf("copy progress data: %w", err)
	}
	if _, err = tx.Exec(`DROP TABLE media_progress`); err != nil {
		return fmt.Errorf("drop legacy progress table: %w", err)
	}
	if _, err = tx.Exec(`ALTER TABLE media_progress_v2 RENAME TO media_progress`); err != nil {
		return fmt.Errorf("activate upgraded progress table: %w", err)
	}
	if _, err = tx.Exec(`PRAGMA user_version = 2`); err != nil {
		return fmt.Errorf("record progress schema version: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit progress migration: %w", err)
	}
	return nil
}

// migrateOldData migrates data from old anime_progress table to new media_progress table
func migrateOldData(db *sql.DB) {
	// Check if old table exists
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type=? AND name=?", "table", "anime_progress").Scan(&tableName)
	if err != nil {
		return // Old table doesn't exist, nothing to migrate
	}

	// Migrate data: for each allanime_id, keep the entry with highest playback_time
	_, err = db.Exec(`
		INSERT OR REPLACE INTO media_progress (allanime_id, anilist_id, episode_number, playback_time, duration, title, media_type, last_updated)
		SELECT 
			allanime_id,
			MAX(anilist_id),
			episode_number,
			MAX(playback_time),
			MAX(duration),
			title,
			CASE WHEN title LIKE '[Movies/TV]%' OR title LIKE '[Movie]%' THEN 'movie' ELSE 'anime' END,
			MAX(last_updated)
		FROM anime_progress
		GROUP BY allanime_id
	`)
	if err != nil {
		fmt.Printf("Warning: migration failed: %v\n", err)
		return
	}

	// Drop old table after successful migration
	_, _ = db.Exec("DROP TABLE IF EXISTS anime_progress")
	fmt.Println("✓ Migrated tracking data to new format (movies + anime support)")
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Preparação de Statements                                                  │
*────────────────────────────────────────────────────────────────────────────
*/
type preparedStatements struct {
	upsert *sql.Stmt
	get    *sql.Stmt
	all    *sql.Stmt
	delete *sql.Stmt
}

func prepareStatements(db *sql.DB) (*preparedStatements, error) {
	// New schema: allanime_id is the primary key (unique per content)
	upsert, err := db.Prepare(`INSERT INTO media_progress (
		allanime_id,
		anilist_id,
		series_key,
		series_url,
		series_title,
		source,
		episode_url,
		total_episodes,
		episode_number, 
		playback_time, 
		duration, 
		title,
		media_type,
		completed,
		last_updated
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(allanime_id) DO UPDATE SET
		anilist_id = CASE WHEN excluded.anilist_id > 0 THEN excluded.anilist_id ELSE media_progress.anilist_id END,
		series_key = CASE WHEN excluded.series_key != '' THEN excluded.series_key ELSE media_progress.series_key END,
		series_url = CASE WHEN excluded.series_url != '' THEN excluded.series_url ELSE media_progress.series_url END,
		series_title = CASE WHEN excluded.series_title != '' THEN excluded.series_title ELSE media_progress.series_title END,
		source = CASE WHEN excluded.source != '' THEN excluded.source ELSE media_progress.source END,
		episode_url = CASE WHEN excluded.episode_url != '' THEN excluded.episode_url ELSE media_progress.episode_url END,
		total_episodes = CASE WHEN excluded.total_episodes > 0 THEN excluded.total_episodes ELSE media_progress.total_episodes END,
		episode_number = excluded.episode_number,
		playback_time = excluded.playback_time,
		duration = excluded.duration,
		title = excluded.title,
		media_type = excluded.media_type,
		completed = CASE WHEN excluded.completed = 1 THEN 1 ELSE media_progress.completed END,
		last_updated = excluded.last_updated`)

	if err != nil {
		return nil, fmt.Errorf("upsert preparation failed: %w", err)
	}

	// Get by allanime_id only (works for both movies and anime)
	get, err := db.Prepare(`SELECT
		allanime_id, series_key, series_url, series_title, source, episode_url, total_episodes,
		episode_number, playback_time, duration, title, media_type, completed, last_updated
	FROM media_progress 
	WHERE allanime_id = ? OR episode_url = ?
	ORDER BY last_updated DESC LIMIT 1`)

	if err != nil {
		return nil, fmt.Errorf("get preparation failed: %w", err)
	}

	all, err := db.Prepare(`SELECT 
		anilist_id, allanime_id, series_key, series_url, series_title, source, episode_url, total_episodes,
		episode_number, playback_time, duration, title, media_type, completed, last_updated
	FROM media_progress ORDER BY last_updated DESC`)

	if err != nil {
		return nil, fmt.Errorf("all preparation failed: %w", err)
	}

	deleteStmt, err := db.Prepare(`DELETE FROM media_progress
		WHERE allanime_id = ?`)

	if err != nil {
		return nil, fmt.Errorf("delete preparation failed: %w", err)
	}

	return &preparedStatements{
		upsert: upsert,
		get:    get,
		all:    all,
		delete: deleteStmt,
	}, nil
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Operações Principais                                                      │
*────────────────────────────────────────────────────────────────────────────
*/
func (t *LocalTracker) UpdateProgress(a Anime) error {
	// Safety check for when tracker is not initialized
	if t == nil || t.db == nil || t.upsertPS == nil {
		return ErrTrackerNotInited
	}

	if a.Duration < 0 {
		return fmt.Errorf("invalid duration value (%d): must not be negative", a.Duration)
	}

	// Validate playback time (shouldn't be negative)
	if a.PlaybackTime < 0 {
		a.PlaybackTime = 0
	}

	// Determine media type from title if not set
	if a.MediaType == "" {
		if strings.Contains(a.Title, "[Movies/TV]") || strings.Contains(a.Title, "[Movie]") {
			a.MediaType = "movie"
		} else {
			a.MediaType = "anime"
		}
	}
	if a.LastUpdated.IsZero() {
		a.LastUpdated = time.Now()
	}

	// New order: allanime_id first (primary key)
	_, err := t.upsertPS.Exec(
		a.AllanimeID,
		a.AnilistID,
		a.SeriesKey,
		a.SeriesURL,
		a.SeriesTitle,
		a.Source,
		a.EpisodeURL,
		a.TotalEpisodes,
		a.EpisodeNumber,
		a.PlaybackTime,
		a.Duration,
		a.Title,
		a.MediaType,
		a.Completed,
		a.LastUpdated.Unix(),
	)
	return err
}

// GetAnime retrieves tracking data by allanime_id (works for both movies and anime)
// The anilistID parameter is kept for backwards compatibility but is ignored
func (t *LocalTracker) GetAnime(anilistID int, allanimeID string) (*Anime, error) {
	// Safety check for when tracker is not initialized
	if t == nil || t.db == nil || t.getPS == nil {
		return nil, ErrTrackerNotInited
	}

	var a Anime
	var ts int64

	// Query by allanime_id only (primary key)
	err := t.getPS.QueryRow(allanimeID, allanimeID).Scan(
		&a.AllanimeID,
		&a.SeriesKey,
		&a.SeriesURL,
		&a.SeriesTitle,
		&a.Source,
		&a.EpisodeURL,
		&a.TotalEpisodes,
		&a.EpisodeNumber,
		&a.PlaybackTime,
		&a.Duration,
		&a.Title,
		&a.MediaType,
		&a.Completed,
		&ts,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}

	a.AnilistID = anilistID
	a.LastUpdated = time.Unix(ts, 0)
	return &a, nil
}

func (t *LocalTracker) GetAllAnime() ([]Anime, error) {
	// Safety check for when tracker is not initialized
	if t == nil || t.db == nil || t.allPS == nil {
		return nil, ErrTrackerNotInited
	}

	rows, err := t.allPS.Query()
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	list := make([]Anime, 0, avgAnimePerUser)
	for rows.Next() {
		var a Anime
		var ts int64
		if err := rows.Scan(
			&a.AnilistID,
			&a.AllanimeID,
			&a.SeriesKey,
			&a.SeriesURL,
			&a.SeriesTitle,
			&a.Source,
			&a.EpisodeURL,
			&a.TotalEpisodes,
			&a.EpisodeNumber,
			&a.PlaybackTime,
			&a.Duration,
			&a.Title,
			&a.MediaType,
			&a.Completed,
			&ts,
		); err != nil {
			return nil, fmt.Errorf("row scan failed: %w", err)
		}
		a.LastUpdated = time.Unix(ts, 0)
		list = append(list, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return list, nil
}

// SetCompleted updates or creates one episode marker.
func (t *LocalTracker) SetCompleted(a Anime, completed bool) error {
	if err := t.UpdateProgress(a); err != nil {
		return err
	}
	_, err := t.db.Exec(`UPDATE media_progress SET completed = ?, last_updated = ? WHERE allanime_id = ?`, completed, time.Now().Unix(), a.AllanimeID)
	return err
}

// DeleteSeries removes all progress for one title.
func (t *LocalTracker) DeleteSeries(seriesKey string) error {
	if t == nil || t.db == nil {
		return ErrTrackerNotInited
	}
	_, err := t.db.Exec(`DELETE FROM media_progress WHERE series_key = ?`, seriesKey)
	return err
}

// ClearHistory removes all watch progress while preserving preferences.
func (t *LocalTracker) ClearHistory() error {
	if t == nil || t.db == nil {
		return ErrTrackerNotInited
	}
	_, err := t.db.Exec(`DELETE FROM media_progress`)
	return err
}

// Autoplay returns the persisted preference; it defaults to enabled.
func (t *LocalTracker) Autoplay() bool {
	if t == nil || t.db == nil {
		return true
	}
	var value string
	if err := t.db.QueryRow(`SELECT value FROM settings WHERE key = 'autoplay'`).Scan(&value); err != nil {
		return true
	}
	return value != "false"
}

// SetAutoplay persists the autoplay preference.
func (t *LocalTracker) SetAutoplay(enabled bool) error {
	if t == nil || t.db == nil {
		return ErrTrackerNotInited
	}
	_, err := t.db.Exec(`INSERT INTO settings(key, value) VALUES('autoplay', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprint(enabled))
	return err
}

// GroupSeries converts recent episode rows into series-first hub entries.
func GroupSeries(entries []Anime) []Series {
	groups := make([]Series, 0)
	indexes := make(map[string]int)
	for _, entry := range entries {
		if entry.SeriesKey == "" || entry.SeriesURL == "" || entry.SeriesTitle == "" {
			continue
		}
		index, ok := indexes[entry.SeriesKey]
		if !ok {
			index = len(groups)
			indexes[entry.SeriesKey] = index
			groups = append(groups, Series{
				Key: entry.SeriesKey, URL: entry.SeriesURL, Title: entry.SeriesTitle,
				Source: entry.Source, MediaType: entry.MediaType, AnilistID: entry.AnilistID,
				TotalEpisodes: entry.TotalEpisodes, LastUpdated: entry.LastUpdated,
			})
		}
		if entry.TotalEpisodes > groups[index].TotalEpisodes {
			groups[index].TotalEpisodes = entry.TotalEpisodes
		}
		groups[index].Episodes = append(groups[index].Episodes, entry)
	}
	for i := range groups {
		latestIncompleteEpisode := 0
		var latestIncompleteAt time.Time
		maxEpisode := 0
		completed := make(map[int]bool, groups[i].TotalEpisodes)
		for j := range groups[i].Episodes {
			episode := &groups[i].Episodes[j]
			if episode.EpisodeNumber > maxEpisode {
				maxEpisode = episode.EpisodeNumber
			}
			completed[episode.EpisodeNumber] = episode.Completed
			if !episode.Completed && episode.PlaybackTime > 0 && (latestIncompleteEpisode == 0 || episode.LastUpdated.After(latestIncompleteAt)) {
				latestIncompleteEpisode = episode.EpisodeNumber
				latestIncompleteAt = episode.LastUpdated
			}
		}
		sort.Slice(groups[i].Episodes, func(a, b int) bool {
			return groups[i].Episodes[a].EpisodeNumber < groups[i].Episodes[b].EpisodeNumber
		})
		if latestIncompleteEpisode > 0 {
			groups[i].ContinueEpisode = latestIncompleteEpisode
		} else {
			if groups[i].TotalEpisodes > 0 {
				for episode := 1; episode <= groups[i].TotalEpisodes; episode++ {
					if !completed[episode] {
						groups[i].ContinueEpisode = episode
						break
					}
				}
			}
			if groups[i].ContinueEpisode == 0 {
				groups[i].ContinueEpisode = maxEpisode
				if groups[i].MediaType != "movie" && groups[i].TotalEpisodes == 0 {
					groups[i].ContinueEpisode++
				}
			}
		}
	}
	return groups
}

// DeleteAnime removes tracking data by allanime_id
// The anilistID parameter is kept for backwards compatibility but is ignored
func (t *LocalTracker) DeleteAnime(anilistID int, allanimeID string) error {
	_, err := t.deletePS.Exec(allanimeID)
	return err
}

/*
────────────────────────────────────────────────────────────────────────────*
│  Finalização                                                               │
*────────────────────────────────────────────────────────────────────────────
*/
func (t *LocalTracker) Close() error {
	if t == nil {
		return nil
	}
	var finalErr error

	closeStmt := func(stmt *sql.Stmt, name string) {
		if stmt != nil {
			if err := stmt.Close(); err != nil {
				finalErr = fmt.Errorf("%s statement close error: %w", name, err)
			}
		}
	}

	closeStmt(t.upsertPS, "upsert")
	closeStmt(t.getPS, "get")
	closeStmt(t.allPS, "all")
	closeStmt(t.deletePS, "delete")

	if err := t.db.Close(); err != nil {
		finalErr = fmt.Errorf("database close error: %w", err)
	}

	return finalErr
}

func init() {
	// This will be replaced at build time with false if CGO is disabled
	// When using CGO_ENABLED=0
	IsCgoEnabled = isCgoEnabled()
	NewLocalTracker = newLocalTrackerImpl // Initialize the public variable
}

// The implementation of isCgoEnabled is defined in local_cgo.go and local_nocgo.go
// based on build tags. We don't define it here to avoid duplicate declarations.
