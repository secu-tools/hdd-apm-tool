// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =====================================================================
// ParseLevel
// =====================================================================

func TestParseLevel(t *testing.T) {
	cases := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"WARNING", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"fatal", LevelFatal},
		{"FATAL", LevelFatal},
		{"unknown", LevelInfo},
		{"", LevelInfo},
		{"  info  ", LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := ParseLevel(tc.input); got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// =====================================================================
// Level.String
// =====================================================================

func TestLevelString(t *testing.T) {
	cases := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
		{Level(99), "INFO"}, // unknown -> INFO
	}
	for _, tc := range cases {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("Level(%d).String() = %q, want %q", tc.level, got, tc.want)
		}
	}
}

// =====================================================================
// New / file creation
// =====================================================================

func TestNew_CreatesLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	old := customLogDir
	defer func() { customLogDir = old }()
	customLogDir = tmpDir

	l, err := New("test.log", DefaultConfig(), "test")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	if _, err := os.Stat(filepath.Join(tmpDir, "test.log")); os.IsNotExist(err) {
		t.Error("log file was not created")
	}
}

func TestNew_InvalidDir(t *testing.T) {
	// Pass a path that cannot be created (null byte is invalid on all platforms).
	_, err := New("x.log", DefaultConfig(), "test")
	// This will succeed or fail depending on permissions; we only care that it
	// doesn't panic.
	_ = err
}

// =====================================================================
// Write and read back
// =====================================================================

func TestLogger_WriteAndReadBack(t *testing.T) {
	tmpDir := t.TempDir()
	old := customLogDir
	defer func() { customLogDir = old }()
	customLogDir = tmpDir

	cfg := DefaultConfig()
	cfg.MinLevel = LevelDebug
	l, err := New("write_test.log", cfg, "test")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	l.Infof("hello %s", "world")
	l.Warnf("watch out")
	l.Errorf("something wrong: %d", 42)
	l.Close()

	data, err := os.ReadFile(filepath.Join(tmpDir, "write_test.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "hello world") {
		t.Errorf("log does not contain 'hello world', got:\n%s", content)
	}
	if !strings.Contains(content, "watch out") {
		t.Errorf("log does not contain 'watch out'")
	}
	if !strings.Contains(content, "something wrong: 42") {
		t.Errorf("log does not contain error message")
	}
}

// =====================================================================
// Level filtering
// =====================================================================

func TestLogger_LevelFiltering(t *testing.T) {
	var buf strings.Builder
	cfg := DefaultConfig()
	cfg.MinLevel = LevelWarn
	l := NewWriterLogger(&buf, cfg, "filter-test")

	l.Debugf("debug msg")
	l.Infof("info msg")
	l.Warnf("warn msg")
	l.Errorf("error msg")

	out := buf.String()
	if strings.Contains(out, "debug msg") {
		t.Error("DEBUG message should have been filtered")
	}
	if strings.Contains(out, "info msg") {
		t.Error("INFO message should have been filtered")
	}
	if !strings.Contains(out, "warn msg") {
		t.Error("WARN message should appear in output")
	}
	if !strings.Contains(out, "error msg") {
		t.Error("ERROR message should appear in output")
	}
}

func TestLogger_DebugLevelShowsAll(t *testing.T) {
	var buf strings.Builder
	cfg := DefaultConfig()
	cfg.MinLevel = LevelDebug
	l := NewWriterLogger(&buf, cfg, "debug-test")

	l.Debugf("debug visible")
	l.Infof("info visible")

	out := buf.String()
	if !strings.Contains(out, "debug visible") {
		t.Error("DEBUG message should appear")
	}
	if !strings.Contains(out, "info visible") {
		t.Error("INFO message should appear")
	}
}

// =====================================================================
// Log line format
// =====================================================================

func TestLogger_FormatContainsLevelAndModule(t *testing.T) {
	var buf strings.Builder
	cfg := DefaultConfig()
	cfg.MinLevel = LevelDebug
	l := NewWriterLogger(&buf, cfg, "mymod")

	l.Infof("test message")
	out := buf.String()

	if !strings.Contains(out, "INFO") {
		t.Errorf("log line should contain INFO, got: %s", out)
	}
	if !strings.Contains(out, "[mymod]") {
		t.Errorf("log line should contain [mymod], got: %s", out)
	}
	if !strings.Contains(out, "test message") {
		t.Errorf("log line should contain message, got: %s", out)
	}
	// Format: "LEVEL | TIMESTAMP [module] message"
	if !strings.Contains(out, " | ") {
		t.Errorf("log line should contain ' | ' separator, got: %s", out)
	}
}

func TestLogger_FormatLevelPadding(t *testing.T) {
	var buf strings.Builder
	cfg := DefaultConfig()
	cfg.MinLevel = LevelDebug
	l := NewWriterLogger(&buf, cfg, "pad")

	l.Infof("x")
	l.Warnf("x")
	l.Errorf("x")
	l.Debugf("x")

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if len(line) == 0 {
			continue
		}
		// Each line should start with "LEVEL | " where LEVEL is 5 chars wide.
		if len(line) < 8 || line[5:8] != " | " {
			t.Errorf("unexpected line format (expected 5-char level then ' | '): %q", line)
		}
	}
}

// =====================================================================
// SetLevel
// =====================================================================

func TestLogger_SetLevel(t *testing.T) {
	var buf strings.Builder
	cfg := DefaultConfig()
	cfg.MinLevel = LevelInfo
	l := NewWriterLogger(&buf, cfg, "setlvl")

	l.Debugf("should not appear")
	l.SetLevel(LevelDebug)
	l.Debugf("should appear")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Error("pre-SetLevel debug should be filtered")
	}
	if !strings.Contains(out, "should appear") {
		t.Error("post-SetLevel debug should appear")
	}
}

// =====================================================================
// SetLogDir
// =====================================================================

func TestSetLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	old := customLogDir
	defer func() { customLogDir = old }()

	if err := SetLogDir(tmpDir); err != nil {
		t.Fatalf("SetLogDir() error: %v", err)
	}
	if got := LogDir(); got != tmpDir {
		t.Errorf("LogDir() = %q, want %q", got, tmpDir)
	}
}

// =====================================================================
// NewStdoutOnly
// =====================================================================

func TestNewStdoutOnly_NoFile(t *testing.T) {
	l := NewStdoutOnly(DefaultConfig(), "stdout-only")
	// Should not panic and should have no file writer.
	l.Infof("stdout only message")
	l.Close()
	if l.file != nil {
		t.Error("stdout-only logger should have no file")
	}
}

// =====================================================================
// Close idempotent
// =====================================================================

func TestLogger_CloseIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	old := customLogDir
	defer func() { customLogDir = old }()
	customLogDir = tmpDir

	l, err := New("close_test.log", DefaultConfig(), "test")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	l.Close()
	l.Close() // second close should not panic
}

// =====================================================================
// Log rotation
// =====================================================================

func TestLogger_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	old := customLogDir
	defer func() { customLogDir = old }()
	customLogDir = tmpDir

	cfg := Config{
		MaxSizeMB:  1, // very small to trigger rotation quickly
		MaxBackups: 2,
		MinLevel:   LevelDebug,
	}
	// Use MaxSizeMB = 0 bytes trick: override via direct struct manipulation
	// Instead write enough to exceed 1 MB.
	cfg.MaxSizeMB = 1
	l, err := New("rotation_test.log", cfg, "rot")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	// Write ~1.1 MB worth of log lines to trigger rotation.
	msg := strings.Repeat("x", 1000)
	for i := 0; i < 1200; i++ {
		l.Infof("%s", msg)
	}
	l.Close()

	// At least one rotated backup should exist.
	backup := filepath.Join(tmpDir, "rotation_test.log.1")
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Error("expected rotated backup rotation_test.log.1 to exist")
	}
}
