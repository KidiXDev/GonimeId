package util

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/KidiXDev/GonimeId/internal/tui"
	"github.com/charmbracelet/colorprofile"
)

var Logger *log.Logger

var consoleLogMu sync.Mutex
var consoleLogSuppressions int

// fileLogger is a separate logger that writes plain text to the log file (no ANSI codes)
var fileLogger *log.Logger

// logFile holds the reference to the open log file so it can be closed on cleanup
var logFile *os.File

// LogFilePath stores the path to the current debug log file (exported for user visibility)
var LogFilePath string

// PrintSavedLocation prints a colored message showing where a downloaded file
// or directory was saved. The label is shown in purple and the path in light purple.
// Falls back to plain text when the console cannot render ANSI safely.
func PrintSavedLocation(label, path string) {
	if !tui.SupportsANSI(os.Stdout) {
		fmt.Printf("%s %s\n", label, path)
		return
	}
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6366F1")).
		Bold(true)
	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA"))
	fmt.Printf("%s %s\n", labelStyle.Render(label), pathStyle.Render(path))
}

// getColoredPrefix returns a styled logger prefix, or plain text when the
// console cannot render ANSI (classic Windows cmd.exe without VT).
func getColoredPrefix() string {
	return prefixForProfile(tui.ConsoleColorProfile(os.Stderr))
}

// prefixForProfile builds the GonimeId logger prefix for the given profile.
// Exported-to-tests via package scope so regressions cannot reintroduce
// baked-in TrueColor ANSI on ASCII/NoTTY profiles.
func prefixForProfile(p colorprofile.Profile) string {
	if p <= colorprofile.ASCII {
		return "GonimeId"
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#6366F1")).
		Bold(true).
		Padding(0, 1).
		MarginRight(1)
	return style.Render("GonimeId")
}

// GetLogDir returns the platform-specific directory for storing log files.
// The paths are chosen to be easily accessible for non-technical users:
//   - Windows: %LOCALAPPDATA%\GonimeId\logs
//   - macOS:   ~/Library/Logs/GonimeId
//   - Linux:   ~/.local/share/gonimeid/logs
func GetLogDir() string {
	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, _ := os.UserHomeDir()
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "GonimeId", "logs")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Logs", "GonimeId")
	default: // linux and others
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "gonimeid", "logs")
	}
}

// initFileLogger creates the log file and initializes the file-only logger.
// Each run creates a unique log file (date + time) so logs are never overwritten or mixed.
// Returns the file handle (caller must close) or nil on error.
func initFileLogger() *os.File {
	logDir := GetLogDir()
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create log directory %s: %v\n", logDir, err)
		return nil
	}

	// Each session gets a unique file: gonimeid_2026-02-27_15-44-10.log
	// This ensures multiple runs per day never collide or mix logs
	now := time.Now()
	filename := fmt.Sprintf("gonimeid_%s.log", now.Format("2006-01-02_15-04-05"))
	logPath := filepath.Join(logDir, filename)

	// In the unlikely event of two runs in the same second, append a counter
	if _, err := os.Stat(logPath); err == nil {
		for i := 2; i <= 100; i++ {
			candidate := filepath.Join(logDir, fmt.Sprintf("gonimeid_%s_%d.log", now.Format("2006-01-02_15-04-05"), i))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				logPath = candidate
				break
			}
		}
	}

	LogFilePath = logPath

	// Create new file exclusively for this session (O_CREATE|O_WRONLY, no O_APPEND needed since it's a fresh file)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600) // #nosec G304
	if err != nil {
		// Fallback to append mode if O_EXCL fails for any reason
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open log file %s: %v\n", logPath, err)
			return nil
		}
	}

	// Write session header
	header := fmt.Sprintf("===== GonimeId Debug Session — %s =====\n\n", now.Format("2006-01-02 15:04:05"))
	_, _ = f.WriteString(header)

	// Create a plain-text logger that writes to the file (no ANSI colors)
	fileLogger = log.NewWithOptions(f, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
		TimeFormat:      "15:04:05",
		Prefix:          "GonimeId",
	})
	fileLogger.SetLevel(log.DebugLevel)
	fileLogger.SetColorProfile(colorprofile.ASCII) // no colors in the file

	return f
}

// InitLogger initializes the beautiful charmbracelet logger.
// When debug mode is enabled, logs are written to a file on disk
// so users can easily share them for troubleshooting.
// The console logger always stays at InfoLevel to avoid corrupting
// interactive TUI components (menus, prompts, etc.).
func InitLogger() {
	// Safe profile: enables VT on Windows when possible; falls back to ASCII
	// when classic cmd.exe cannot process ANSI (avoids raw ←[38;2;...m garbage).
	profile := tui.ConsoleColorProfile(os.Stderr)

	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: IsDebug,
		TimeFormat:      "15:04:05",
		Prefix:          prefixForProfile(profile),
	})

	// Console logger is always InfoLevel to keep the terminal clean for TUI.
	// Debug-level messages are routed exclusively to the log file.
	Logger.SetLevel(log.InfoLevel)
	Logger.SetColorProfile(profile)

	if IsDebug {
		// Initialize file logging — all debug output goes here
		logFile = initFileLogger()
		if logFile != nil {
			RegisterCleanup(CloseLogFile)
			// Reset first so the banner starts on its own fresh line rather
			// than risking a glue-on with whatever column the shell's
			// prompt left the cursor at when this process started.
			tui.ResetTerminal()
			showDebugBanner()
		} else {
			Logger.Info("Debug mode enabled (file logging unavailable — logs will appear in console)")
			// Fallback: if we can't write to a file, allow debug on console
			Logger.SetLevel(log.DebugLevel)
			Logger.SetReportCaller(true)
		}
	}
}

// EchoLastError prints the newest ERROR recorded in the last few seconds to
// the console. The interactive session silences console output while screens
// are up, so an error logged right before the app exits would otherwise only
// exist in the log file; the caller invokes this after restoring the console.
func EchoLastError() {
	if Logger == nil {
		return
	}
	if e, ok := tui.LastNotice(5 * time.Second); ok && e.Level == tui.LogError {
		Logger.Error(e.Message)
	}
}

// withKeyvals renders structured key/value pairs the way the console line
// would, so the in-app log overlay shows the same text.
func withKeyvals(msg string, keyvals []any) string {
	if len(keyvals) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i+1 < len(keyvals); i += 2 {
		fmt.Fprintf(&b, " %v=%v", keyvals[i], keyvals[i+1])
	}
	if len(keyvals)%2 == 1 {
		fmt.Fprintf(&b, " %v", keyvals[len(keyvals)-1])
	}
	return b.String()
}

// SuppressConsoleLogging temporarily silences the console logger while keeping
// file logging available through the util logging helpers. This prevents async
// logs from corrupting interactive progress bars.
func SuppressConsoleLogging() func() {
	consoleLogMu.Lock()
	if Logger != nil {
		if consoleLogSuppressions == 0 {
			Logger.SetOutput(io.Discard)
		}
		consoleLogSuppressions++
	}
	consoleLogMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			consoleLogMu.Lock()
			defer consoleLogMu.Unlock()
			if Logger == nil || consoleLogSuppressions == 0 {
				return
			}
			consoleLogSuppressions--
			if consoleLogSuppressions == 0 {
				Logger.SetOutput(os.Stderr)
			}
		})
	}
}

// showDebugBanner prints a styled notice so the user knows where to find
// the debug log and how to follow it in real-time from another terminal.
// The follow command adapts to the user's OS. Plain text when ANSI unsafe.
func showDebugBanner() {
	// Pick the right "follow file" command for each OS
	var followCmd string
	switch runtime.GOOS {
	case "windows":
		followCmd = fmt.Sprintf("Get-Content -Wait -Tail 50 %q", LogFilePath)
	default: // linux, darwin, etc.
		followCmd = fmt.Sprintf("tail -f %q", LogFilePath)
	}

	if !tui.SupportsANSI(os.Stderr) {
		fmt.Fprintf(os.Stderr, " DEBUG  Debug log → %s\n       %s (run in another terminal to follow live)\n",
			LogFilePath, followCmd)
		return
	}

	banner := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#6366F1")).
		Bold(true).
		Padding(0, 1).
		Render(" DEBUG ")

	path := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")).
		Italic(true).
		Render(LogFilePath)

	tailCmd := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FCD34D")).
		Bold(true).
		Render(followCmd)

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Render("(run in another terminal to follow live)")

	fmt.Fprintf(os.Stderr, "%s Debug log → %s\n       %s %s\n", banner, path, tailCmd, hint)
}

// CloseLogFile flushes and closes the debug log file
func CloseLogFile() {
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
		logFile = nil
	}
}

// writeToFile writes a message to the file logger if active
func writeToFile(level log.Level, msg string, keyvals ...any) {
	if fileLogger == nil {
		return
	}
	switch level {
	case log.DebugLevel:
		fileLogger.Debug(msg, keyvals...)
	case log.InfoLevel:
		fileLogger.Info(msg, keyvals...)
	case log.WarnLevel:
		fileLogger.Warn(msg, keyvals...)
	case log.ErrorLevel:
		fileLogger.Error(msg, keyvals...)
	case log.FatalLevel:
		fileLogger.Error(msg, keyvals...) // don't call Fatal on file logger to avoid double-exit
	}
}

// GetLogFileWriter returns the log file as an io.Writer for external use (e.g., capturing subprocess output).
// Returns nil if file logging is not active.
func GetLogFileWriter() io.Writer {
	if logFile == nil {
		return nil
	}
	return logFile
}

// Debug logs a debug message (only when debug mode is enabled).
// Debug messages are written exclusively to the log file to avoid
// corrupting interactive TUI elements on the terminal.
func Debug(msg any, keyvals ...any) {
	if IsDebug {
		formatted := fmt.Sprintf("%v", msg)
		tui.AppendLog(tui.LogDebug, withKeyvals(formatted, keyvals))
		if fileLogger != nil {
			writeToFile(log.DebugLevel, formatted, keyvals...)
		} else if Logger != nil {
			// Fallback: no log file, write to console
			Logger.Debug(formatted, keyvals...)
		}
	}
}

// Info logs an info message
func Info(msg any, keyvals ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf("%v", msg)
		tui.AppendLog(tui.LogInfo, withKeyvals(formatted, keyvals))
		Logger.Info(formatted, keyvals...)
		writeToFile(log.InfoLevel, formatted, keyvals...)
	}
}

// Warn logs a warning message
func Warn(msg any, keyvals ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf("%v", msg)
		tui.AppendLog(tui.LogWarn, withKeyvals(formatted, keyvals))
		Logger.Warn(formatted, keyvals...)
		writeToFile(log.WarnLevel, formatted, keyvals...)
	}
}

// Error logs an error message
func Error(msg any, keyvals ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf("%v", msg)
		tui.AppendLog(tui.LogError, withKeyvals(formatted, keyvals))
		Logger.Error(formatted, keyvals...)
		writeToFile(log.ErrorLevel, formatted, keyvals...)
	}
}

// Fatal logs a fatal message and exits
func Fatal(msg any, keyvals ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf("%v", msg)
		writeToFile(log.FatalLevel, formatted, keyvals...)
		CloseLogFile() // ensure file is flushed before exit
		Logger.Fatal(formatted, keyvals...)
	}
}

// Debugf logs a formatted debug message (only when debug mode is enabled).
// Debug messages are written exclusively to the log file.
func Debugf(format string, args ...any) {
	if IsDebug {
		formatted := fmt.Sprintf(format, args...)
		tui.AppendLog(tui.LogDebug, formatted)
		if fileLogger != nil {
			writeToFile(log.DebugLevel, formatted)
		} else if Logger != nil {
			// Fallback: no log file, write to console
			Logger.Debug(formatted)
		}
	}
}

// Infof logs a formatted info message
func Infof(format string, args ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf(format, args...)
		tui.AppendLog(tui.LogInfo, formatted)
		Logger.Info(formatted)
		writeToFile(log.InfoLevel, formatted)
	}
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf(format, args...)
		tui.AppendLog(tui.LogWarn, formatted)
		Logger.Warn(formatted)
		writeToFile(log.WarnLevel, formatted)
	}
}

// Errorf logs a formatted error message
func Errorf(format string, args ...any) {
	if Logger != nil {
		formatted := fmt.Sprintf(format, args...)
		tui.AppendLog(tui.LogError, formatted)
		Logger.Error(formatted)
		writeToFile(log.ErrorLevel, formatted)
	}
}
