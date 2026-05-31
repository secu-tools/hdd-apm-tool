// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build darwin

package service

import (
	"strings"
	"testing"
)

// =====================================================================
// buildDarwinPlist output format
// =====================================================================

func TestBuildDarwinPlist_ContainsLabel(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "com.secu-tools.hdd-apm-tool") {
		t.Errorf("plist missing expected label: %s", plist)
	}
}

func TestBuildDarwinPlist_ContainsExe(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	exe := "/usr/local/bin/hdd-apm-tool"
	plist := buildDarwinPlist(cfg, exe, darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, exe) {
		t.Errorf("plist missing exe path %q", exe)
	}
}

func TestBuildDarwinPlist_ContainsRunAtLoad(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "<key>RunAtLoad</key>") {
		t.Error("plist missing RunAtLoad key")
	}
	if !strings.Contains(plist, "<true/>") {
		t.Error("plist missing <true/> for RunAtLoad")
	}
}

func TestBuildDarwinPlist_KeepAliveIsFalse(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Error("plist missing KeepAlive key")
	}
	if !strings.Contains(plist, "<false/>") {
		t.Error("plist missing <false/> for KeepAlive (should be false for one-shot tool)")
	}
}

func TestBuildDarwinPlist_ContainsAPMLevel(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "<string>--apm</string>") {
		t.Error("plist missing --apm argument")
	}
	if !strings.Contains(plist, "<string>128</string>") {
		t.Error("plist missing APM level value")
	}
}

func TestBuildDarwinPlist_ContainsServiceFlag(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 254}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "<string>--service</string>") {
		t.Error("plist missing --service flag")
	}
}

func TestBuildDarwinPlist_DefaultLogDir(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, darwinDefaultLogDir) {
		t.Errorf("plist missing default log dir %q", darwinDefaultLogDir)
	}
	if !strings.Contains(plist, "StandardOutPath") {
		t.Error("plist missing StandardOutPath key")
	}
	if !strings.Contains(plist, "StandardErrorPath") {
		t.Error("plist missing StandardErrorPath key")
	}
}

func TestBuildDarwinPlist_CustomLogDir(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128, LogDir: "/var/log/my-apm"}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		"/var/log/my-apm", darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "/var/log/my-apm") {
		t.Errorf("plist missing custom log dir, got: %s", plist)
	}
}

func TestBuildDarwinPlist_WithLabel(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128, DisplayLabel: "work"}
	name := cfg.ServiceName() // hdd-apm-tool_work
	label := darwinLabelPrefix + name
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, label)
	if !strings.Contains(plist, "com.secu-tools.hdd-apm-tool_work") {
		t.Errorf("plist missing labeled service name, got: %s", plist)
	}
}

func TestBuildDarwinPlist_WithLogDir(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 64, LogDir: "/tmp/test-log"}
	label := darwinLabelPrefix + cfg.ServiceName()
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool", "/tmp/test-log", label)
	if !strings.Contains(plist, "--logdir") {
		t.Error("plist missing --logdir arg when LogDir is set")
	}
	if !strings.Contains(plist, "/tmp/test-log") {
		t.Error("plist missing log dir path")
	}
}

func TestBuildDarwinPlist_APMLevelDisable(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 255}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.Contains(plist, "<string>255</string>") {
		t.Error("plist missing APM disable level (255)")
	}
}

func TestBuildDarwinPlist_IsValidXML(t *testing.T) {
	cfg := ServiceConfig{APMLevel: 128}
	plist := buildDarwinPlist(cfg, "/usr/local/bin/hdd-apm-tool",
		darwinDefaultLogDir, darwinLabelPrefix+cfg.ServiceName())
	if !strings.HasPrefix(plist, `<?xml`) {
		t.Error("plist should start with XML declaration")
	}
	if !strings.Contains(plist, `<!DOCTYPE plist`) {
		t.Error("plist missing DOCTYPE declaration")
	}
	if !strings.Contains(plist, "</plist>") {
		t.Error("plist missing closing </plist> tag")
	}
	if !strings.Contains(plist, "<dict>") || !strings.Contains(plist, "</dict>") {
		t.Error("plist missing dict element")
	}
}

// =====================================================================
// xmlEscapeAttr
// =====================================================================

func TestXMLEscapeAttr(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"plain", "plain"},
		{"<tag>", "&lt;tag&gt;"},
		{"a&b", "a&amp;b"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"'apos'", "&apos;apos&apos;"},
		{"/usr/local/bin/hdd-apm-tool", "/usr/local/bin/hdd-apm-tool"},
		{"--apm", "--apm"},
		{"128", "128"},
	}
	for _, tc := range cases {
		got := xmlEscapeAttr(tc.input)
		if got != tc.want {
			t.Errorf("xmlEscapeAttr(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// =====================================================================
// findServicesHddApmToolDarwin smoke test
// =====================================================================

func TestFindServicesHddApmToolDarwin_DoesNotPanic(t *testing.T) {
	// In a clean test environment (CI), /Library/LaunchDaemons may not exist
	// or may not contain any hdd-apm-tool plists. The function must not panic.
	svcs := findServicesHddApmToolDarwin()
	if svcs == nil {
		svcs = []darwinService{}
	}
	for _, s := range svcs {
		if s.label == "" {
			t.Error("found service with empty label")
		}
		if !strings.HasSuffix(s.plistPath, ".plist") {
			t.Errorf("service plist path should end with .plist: %s", s.plistPath)
		}
	}
}

// =====================================================================
// Label and constant format
// =====================================================================

func TestDarwinLabelPrefix(t *testing.T) {
	if darwinLabelPrefix != "com.secu-tools." {
		t.Errorf("darwinLabelPrefix = %q, want %q", darwinLabelPrefix, "com.secu-tools.")
	}
}

func TestDarwinLabelFormat_Default(t *testing.T) {
	cfg := &ServiceConfig{}
	label := darwinLabelPrefix + cfg.ServiceName()
	if label != "com.secu-tools.hdd-apm-tool" {
		t.Errorf("label = %q, want %q", label, "com.secu-tools.hdd-apm-tool")
	}
}

func TestDarwinLabelFormat_WithLabel(t *testing.T) {
	cfg := &ServiceConfig{DisplayLabel: "home"}
	label := darwinLabelPrefix + cfg.ServiceName()
	if label != "com.secu-tools.hdd-apm-tool_home" {
		t.Errorf("label = %q, want %q", label, "com.secu-tools.hdd-apm-tool_home")
	}
}

func TestDarwinDefaultLogDir(t *testing.T) {
	if darwinDefaultLogDir != "/var/log/hdd-apm-tool" {
		t.Errorf("darwinDefaultLogDir = %q, want %q", darwinDefaultLogDir, "/var/log/hdd-apm-tool")
	}
}
