// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

// Package logging provides structured text logging with file rotation,
// level filtering, and platform-aware log paths for APM Tool.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents a log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// ParseLevel converts a string to a Level. Defaults to LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "fatal":
		return LevelFatal
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "INFO"
	}
}

// Config holds logging configuration.
type Config struct {
	MaxSizeMB  int   // Max log file size in MB before rotation (default: 10)
	MaxBackups int   // Max rotated log files to keep (default: 5)
	MinLevel   Level // Minimum log level (default: LevelInfo)
}

// DefaultConfig returns sensible logging defaults.
func DefaultConfig() Config {
	return Config{
		MaxSizeMB:  10,
		MaxBackups: 5,
		MinLevel:   LevelInfo,
	}
}

// Logger writes formatted log lines to stdout and an optional log file.
type Logger struct {
	mu           sync.Mutex
	file         *os.File
	filePath     string
	config       Config
	currentSize  int64
	module       string
	stdoutWriter io.Writer
	fileWriter   io.Writer
}

// package-level state for log directory override and fallback flag.
var (
	customLogDir   string
	logDirFallback bool
)

// SetLogDir overrides the default log directory. The directory is created
// if it does not already exist.
func SetLogDir(dir string) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}
	customLogDir = dir
	return nil
}

// UsingFallbackLogDir returns true when the log directory fell back to an
// executable-relative path because the default system path was not writable.
func UsingFallbackLogDir() bool {
	return logDirFallback
}

// LogDir returns the platform-appropriate log directory.
// Linux/macOS: /var/log/apm-tool (falls back to <exe_dir>/log if no permission)
// Windows:     <exe_dir>/log
func LogDir() string {
	if customLogDir != "" {
		return customLogDir
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		dir := "/var/log/apm-tool"
		if err := os.MkdirAll(dir, 0750); err == nil {
			return dir
		}
		logDirFallback = true
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Join(filepath.Dir(exe), "log")
	if err := os.MkdirAll(dir, 0750); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR | [logging] failed to create log directory %s: %v\n", dir, err)
		return "."
	}
	return dir
}

// New creates a Logger that writes to a log file under LogDir() with rotation
// and to stdout. All entries at or above cfg.MinLevel are emitted.
func New(filename string, cfg Config, module ...string) (*Logger, error) {
	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 10
	}
	if cfg.MaxBackups <= 0 {
		cfg.MaxBackups = 5
	}
	logDir := LogDir()
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", logDir, err)
	}
	l := &Logger{
		config:       cfg,
		module:       resolveModule(module),
		stdoutWriter: os.Stdout,
		filePath:     filepath.Join(logDir, filename),
	}
	if err := l.openFile(); err != nil {
		return nil, err
	}
	return l, nil
}

// NewStdoutOnly creates a Logger that writes only to stdout.
func NewStdoutOnly(cfg Config, module ...string) *Logger {
	return &Logger{
		config:       cfg,
		module:       resolveModule(module),
		stdoutWriter: os.Stdout,
	}
}

// NewWriterLogger creates a Logger that writes to the provided writer.
// Useful for capturing log output in tests.
func NewWriterLogger(w io.Writer, cfg Config, module string) *Logger {
	mod := module
	if mod == "" {
		mod = "test"
	}
	return &Logger{
		config:       cfg,
		module:       mod,
		stdoutWriter: w,
	}
}

func resolveModule(m []string) string {
	if len(m) > 0 && m[0] != "" {
		return m[0]
	}
	return "main"
}

func (l *Logger) openFile() error {
	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", l.filePath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file %s: %w", l.filePath, err)
	}
	l.file = f
	l.fileWriter = f
	l.currentSize = info.Size()
	return nil
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Sync()
		_ = l.file.Close()
		l.file = nil
		l.fileWriter = nil
	}
}

// SetLevel updates the minimum log level at runtime.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.config.MinLevel = level
}

// formatLine builds: "LEVEL | 2006/01/02 15:04:05 [module] message"
func (l *Logger) formatLine(level, msg string) string {
	ts := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%-5s | %s [%s] %s\n", level, ts, l.module, msg)
}

func (l *Logger) log(level Level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if level < l.config.MinLevel {
		return
	}
	line := l.formatLine(level.String(), msg)
	if l.stdoutWriter != nil {
		_, _ = fmt.Fprint(l.stdoutWriter, line)
	}
	if l.fileWriter != nil {
		n, _ := fmt.Fprint(l.fileWriter, line)
		l.currentSize += int64(n)
		if l.config.MaxSizeMB > 0 && l.currentSize >= int64(l.config.MaxSizeMB)*1024*1024 {
			l.rotateUnlocked()
		}
	}
}

// rotateUnlocked rotates the log file. Must be called with l.mu held.
func (l *Logger) rotateUnlocked() {
	if l.file == nil {
		return
	}
	_ = l.file.Sync()
	_ = l.file.Close()
	l.file = nil
	l.fileWriter = nil
	for i := l.config.MaxBackups - 1; i >= 1; i-- {
		_ = os.Rename(
			fmt.Sprintf("%s.%d", l.filePath, i),
			fmt.Sprintf("%s.%d", l.filePath, i+1),
		)
	}
	_ = os.Rename(l.filePath, l.filePath+".1")
	if err := l.openFile(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR | [logging] failed to reopen log after rotation: %v\n", err)
		return
	}
	l.currentSize = 0
}

// Debugf logs at DEBUG level.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LevelDebug, fmt.Sprintf(format, args...))
}

// Infof logs at INFO level.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, fmt.Sprintf(format, args...))
}

// Warnf logs at WARN level.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, fmt.Sprintf(format, args...))
}

// Errorf logs at ERROR level.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LevelError, fmt.Sprintf(format, args...))
}

// Fatalf logs at FATAL level then calls os.Exit(1).
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(LevelFatal, fmt.Sprintf(format, args...))
	os.Exit(1)
}
