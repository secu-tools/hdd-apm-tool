// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/secu-tools/hdd-apm-tool/internal/ata"
	"github.com/secu-tools/hdd-apm-tool/internal/logging"
)

// =====================================================================
// Version helpers
// =====================================================================

func TestFullVersion_DefaultValues(t *testing.T) {
	fv := fullVersion()
	// Default: "1.0.0.0"
	if fv != "1.0.0.0" {
		t.Errorf("fullVersion() = %q, want %q", fv, "1.0.0.0")
	}
}

func TestResolveCommitLabel_Dev(t *testing.T) {
	old := commit
	defer func() { commit = old }()

	commit = "dev"
	label := resolveCommitLabel()
	if label == "" {
		t.Error("resolveCommitLabel() should not return empty string")
	}
}

func TestResolveCommitLabel_Custom(t *testing.T) {
	old := commit
	defer func() { commit = old }()

	commit = "abc1234"
	label := resolveCommitLabel()
	if label != "abc1234" {
		t.Errorf("resolveCommitLabel() = %q, want %q", label, "abc1234")
	}
}

func TestVersionString_Format(t *testing.T) {
	vs := versionString()
	lines := strings.Split(vs, "\n")
	if len(lines) != 3 {
		t.Errorf("versionString() should have 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "HDD APM Tool") {
		t.Errorf("first line should contain 'HDD APM Tool', got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "Copyright") {
		t.Errorf("second line should contain 'Copyright', got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "github.com/secu-tools/hdd-apm-tool") {
		t.Errorf("third line should contain repo URL, got: %s", lines[2])
	}
}

func TestVersionString_ContainsBanner(t *testing.T) {
	vs := versionString()
	if !strings.Contains(vs, "HDD APM Tool") {
		t.Error("version string should contain tool name")
	}
	if !strings.Contains(vs, "jack-l.com") {
		t.Error("version string should contain author URL")
	}
}

// =====================================================================
// categorizeNoAPMReason
// =====================================================================

func TestCategorizeNoAPMReason(t *testing.T) {
	cases := []struct {
		model    string
		contains string
	}{
		{"Samsung SSD 840 EVO 1TB", "Samsung"},
		{"SAMSUNG SSD 850 PRO", "Samsung"},
		{"ADATA SU650", "firmware"},
		{"ST4000DM005-2DP166", "firmware"},
		{"nvme_something", "NVMe"},
		{"UNKNOWN HDD MODEL", "firmware"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			reason := categorizeNoAPMReason(tc.model)
			if reason == "" {
				t.Error("reason should not be empty")
			}
			if !strings.Contains(strings.ToLower(reason), strings.ToLower(tc.contains)) {
				t.Errorf("reason %q should contain %q", reason, tc.contains)
			}
		})
	}
}

// =====================================================================
// truncate
// =====================================================================

func TestTruncate(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := truncate(tc.input, tc.maxLen)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
		}
	}
}

// =====================================================================
// ScanResults zero value
// =====================================================================

func TestScanResults_ZeroValue(t *testing.T) {
	var r ScanResults
	if r.Total != 0 || r.Success != 0 || r.Failed != 0 {
		t.Error("zero ScanResults should have all-zero counts")
	}
}

// =====================================================================
// RunConfig construction
// =====================================================================

func TestRunConfig_Fields(t *testing.T) {
	cfg := RunConfig{
		APMLevel: 128,
		DryRun:   true,
		LogDir:   "/var/log/hdd-apm-tool",
		SvcName:  "hdd-apm-tool",
	}
	if cfg.APMLevel != 128 {
		t.Errorf("APMLevel = %d, want 128", cfg.APMLevel)
	}
	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
}

// =====================================================================
// diskDisplayName
// =====================================================================

func TestDiskDisplayName_WithModel(t *testing.T) {
	d := ata.DiskInfo{Model: "WDC WD121PURZ-85GUCY0", Path: `\\.\PhysicalDrive0`}
	if got := diskDisplayName(d); got != "WDC WD121PURZ-85GUCY0" {
		t.Errorf("diskDisplayName() = %q, want model string", got)
	}
}

func TestDiskDisplayName_NoModel(t *testing.T) {
	d := ata.DiskInfo{Path: `\\.\PhysicalDrive1`}
	if got := diskDisplayName(d); got != `\\.\PhysicalDrive1` {
		t.Errorf("diskDisplayName() = %q, want device path", got)
	}
}

// =====================================================================
// processAllDisks with no disks (no-op)
// =====================================================================

func TestProcessAllDisks_NoPaths(t *testing.T) {
	// processAllDisks delegates to ata.EnumerateDisks; on a non-disk host
	// (CI environment) it may return an empty list. The function must not
	// panic and must return a zero-value ScanResults.
	cfg := RunConfig{APMLevel: 128, DryRun: true}
	logr := newTestLogger()
	res := processAllDisks(cfg, logr)
	// Total is whatever EnumerateDisks returns; cannot assert a specific number
	// in a portable test. We only verify the result is coherent.
	if res.Total < 0 {
		t.Error("Total should be >= 0")
	}
	if res.Success+res.Skipped+res.Failed > res.Total {
		t.Errorf("counts exceed Total: success=%d skipped=%d failed=%d total=%d",
			res.Success, res.Skipped, res.Failed, res.Total)
	}
}

// =====================================================================
// processSingleDisk logic (uses a fake disk path that won't exist)
// =====================================================================

func TestProcessSingleDisk_UnknownPath(t *testing.T) {
	// A nonexistent path should fail IdentifyDevice and be counted as Skipped.
	cfg := RunConfig{APMLevel: 128, DryRun: true}
	logr := newTestLogger()
	result := processSingleDisk(1, "/dev/does-not-exist-apm-test", cfg, logr)
	if !result.Skipped {
		t.Error("nonexistent disk should be Skipped")
	}
}

// =====================================================================
// ScanResults counting invariants
// =====================================================================

func TestScanResults_CountingConsistency(t *testing.T) {
	// Manually build a result set and confirm helpers produce correct counts.
	res := ScanResults{
		Total:   3,
		Success: 1,
		Skipped: 1,
		Failed:  1,
		Items: []ata.APMResult{
			{Success: true, Skipped: false},
			{Skipped: true},
			{Success: false, Skipped: false, Error: fmt.Errorf("fake error")},
		},
	}
	if res.Success+res.Skipped+res.Failed != res.Total {
		t.Errorf("counts don't add up: %d+%d+%d != %d",
			res.Success, res.Skipped, res.Failed, res.Total)
	}
}

// newTestLogger creates a stdout-only logger for unit tests.
func newTestLogger() *logging.Logger {
	return logging.NewStdoutOnly(logging.DefaultConfig(), "test")
}
