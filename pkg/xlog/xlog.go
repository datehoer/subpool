// Package xlog provides leveled logging with optional file output and daily rotate.
package xlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is log severity.
type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "dbg", "d":
		return Debug
	case "warn", "warning", "w":
		return Warn
	case "error", "err", "e":
		return Error
	default:
		return Info
	}
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Warn:
		return "WARN"
	case Error:
		return "ERROR"
	default:
		return "INFO"
	}
}

// Logger is process-wide style logger (safe for concurrent use).
type Logger struct {
	mu       sync.Mutex
	level    Level
	out      io.Writer
	file     *os.File
	filePath string
	day      string // YYYY-MM-DD of open file (for daily rotate)
	alsoStd  bool
}

var std = &Logger{level: Info, out: os.Stderr, alsoStd: true}

// Configure sets global logger: level, optional log file, whether also print to stderr.
func Configure(level string, logFile string, alsoStderr bool) error {
	std.mu.Lock()
	defer std.mu.Unlock()

	std.level = ParseLevel(level)
	std.alsoStd = alsoStderr

	if std.file != nil {
		_ = std.file.Close()
		std.file = nil
	}
	std.filePath = logFile
	std.day = ""

	if logFile == "" {
		std.out = os.Stderr
		return nil
	}

	if err := std.openFileLocked(time.Now()); err != nil {
		return err
	}
	return nil
}

func (l *Logger) openFileLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	path := l.filePath
	// if path is a directory or ends with /, write subpool-YYYY-MM-DD.log inside
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		path = filepath.Join(path, "subpool-"+day+".log")
	} else if strings.HasSuffix(path, string(os.PathSeparator)) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
		path = filepath.Join(path, "subpool-"+day+".log")
	} else {
		// daily rotate: insert date before extension
		ext := filepath.Ext(path)
		base := strings.TrimSuffix(path, ext)
		if ext == "" {
			ext = ".log"
		}
		path = fmt.Sprintf("%s-%s%s", base, day, ext)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
			return err
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file = f
	l.day = day
	if l.alsoStd {
		l.out = io.MultiWriter(os.Stderr, f)
	} else {
		l.out = f
	}
	return nil
}

func (l *Logger) maybeRotateLocked() {
	if l.filePath == "" || l.file == nil {
		return
	}
	day := time.Now().Format("2006-01-02")
	if day != l.day {
		_ = l.openFileLocked(time.Now())
	}
}

func (l *Logger) logf(lv Level, format string, args ...any) {
	if lv < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybeRotateLocked()
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s %s\n", time.Now().Format("2006/01/02 15:04:05"), lv.String(), msg)
	_, _ = io.WriteString(l.out, line)
}

// Debugf logs at debug level.
func Debugf(format string, args ...any) { std.logf(Debug, format, args...) }

// Infof logs at info level.
func Infof(format string, args ...any) { std.logf(Info, format, args...) }

// Warnf logs at warn level.
func Warnf(format string, args ...any) { std.logf(Warn, format, args...) }

// Errorf logs at error level.
func Errorf(format string, args ...any) { std.logf(Error, format, args...) }

// Accessf always writes access lines at INFO (subject to level filter: need Info or lower).
func Accessf(format string, args ...any) { std.logf(Info, "[access] "+format, args...) }

// Level returns current level.
func CurrentLevel() Level {
	std.mu.Lock()
	defer std.mu.Unlock()
	return std.level
}

// Close closes file handle.
func Close() {
	std.mu.Lock()
	defer std.mu.Unlock()
	if std.file != nil {
		_ = std.file.Close()
		std.file = nil
	}
}
