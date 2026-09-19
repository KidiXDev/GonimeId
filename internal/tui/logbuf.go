package tui

import (
	"strings"
	"sync"
	"time"
)

// LogLevel is the severity of a LogEntry.
type LogLevel int

// Levels, in ascending severity.
const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

func (l LogLevel) String() string {
	switch l {
	case LogDebug:
		return "DEBUG"
	case LogInfo:
		return "INFO"
	case LogWarn:
		return "WARN"
	default:
		return "ERROR"
	}
}

// LogEntry is one line the logger recorded during this session.
type LogEntry struct {
	Time    time.Time
	Level   LogLevel
	Message string
}

// logRingSize bounds the in-memory session log; the file log is unbounded.
const logRingSize = 500

var (
	logMu   sync.Mutex
	logRing []LogEntry
)

// AppendLog records a log line for the in-app log overlay. The logger calls
// it for every level it writes; console output is a separate concern — during
// an interactive session it is silenced so nothing prints between screens,
// and this buffer is where those lines go instead.
func AppendLog(level LogLevel, message string) {
	message = singleLine(message)
	if message == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	logRing = append(logRing, LogEntry{Time: time.Now(), Level: level, Message: message})
	if len(logRing) > logRingSize {
		logRing = logRing[len(logRing)-logRingSize:]
	}
}

// RecentLogs returns a copy of the session log, oldest first.
func RecentLogs() []LogEntry {
	logMu.Lock()
	defer logMu.Unlock()
	return append([]LogEntry(nil), logRing...)
}

// LastNotice returns the newest WARN/ERROR entry recorded within the window,
// for the shell footer, or false when there is none.
func LastNotice(within time.Duration) (LogEntry, bool) {
	logMu.Lock()
	defer logMu.Unlock()
	for i := len(logRing) - 1; i >= 0; i-- {
		e := logRing[i]
		if e.Level < LogWarn {
			continue
		}
		if time.Since(e.Time) > within {
			return LogEntry{}, false
		}
		return e, true
	}
	return LogEntry{}, false
}

// ResetLogsForTest empties the session log. Only for tests.
func ResetLogsForTest() {
	logMu.Lock()
	defer logMu.Unlock()
	logRing = nil
}

// formatLogLines renders the newest entries that fit in height rows, oldest
// first, each truncated to width.
func formatLogLines(theme *Theme, entries []LogEntry, width, height int) []string {
	if len(entries) == 0 {
		return []string{theme.Muted.Render("Nothing logged yet.")}
	}
	if len(entries) > height {
		entries = entries[len(entries)-height:]
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		var level string
		switch e.Level {
		case LogError:
			level = theme.Error.Render("ERR ")
		case LogWarn:
			level = theme.Warn.Render("WARN")
		case LogInfo:
			level = theme.Primary.Render("INFO")
		default:
			level = theme.Muted.Render("DBG ")
		}
		line := theme.Muted.Render(e.Time.Format("15:04:05")) + " " + level + " " + theme.Text.Render(e.Message)
		lines = append(lines, fitBlock(line, width, 1))
	}
	return lines
}

// joinLines is strings.Join for the one-liner call sites.
func joinLines(lines []string) string { return strings.Join(lines, "\n") }
