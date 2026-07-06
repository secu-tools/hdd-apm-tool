// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build linux

package service

import (
	"strings"
	"testing"
)

func TestSystemdQuote(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/usr/local/bin/hdd-apm-tool", "/usr/local/bin/hdd-apm-tool"},
		{"--apm", "--apm"},
		{"254", "254"},
		{"", `""`},
		{"/var/log/my logs", `"/var/log/my logs"`},
		{`/path/with"quote`, `"/path/with\"quote"`},
		{`/path/with\backslash`, `"/path/with\\backslash"`},
		{"/path/with'apostrophe", `"/path/with'apostrophe"`},
	}
	for _, tc := range cases {
		got := systemdQuote(tc.input)
		if got != tc.want {
			t.Errorf("systemdQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidateUnitValue(t *testing.T) {
	valid := []string{
		"/var/log/apm-tool",
		"/usr/local/bin/hdd-apm-tool",
		"/path/with spaces/ok",
		"",
	}
	for _, s := range valid {
		if err := validateUnitValue("test value", s); err != nil {
			t.Errorf("validateUnitValue(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{
		"/var/log\nExecStartPre=/bin/evil",
		"/var/log\rfoo",
		"/var/log\tfoo",
		"/var/log\x00foo",
		"/var/log\x7ffoo",
	}
	for _, s := range invalid {
		if err := validateUnitValue("test value", s); err == nil {
			t.Errorf("validateUnitValue(%q) = nil, want error", s)
		}
	}
}

func TestInstallSystemd_RejectsUnitInjection(t *testing.T) {
	// A --logdir value containing a newline must be rejected before any unit
	// file content is generated or written.
	cfg := ServiceConfig{
		APMLevel: 254,
		ExePath:  "/usr/local/bin/hdd-apm-tool",
		LogDir:   "/tmp/x\nExecStartPre=/bin/evil",
	}
	err := installSystemd(cfg)
	if err == nil {
		t.Fatal("installSystemd() should reject a log directory containing a newline")
	}
	if !strings.Contains(err.Error(), "control characters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPathUnderTmp(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/tmp", true},
		{"/tmp/apm-tool", true},
		{"/tmp/build/apm-tool", true},
		{"/etc/apm-tool", false},
		{"/var/log/apm-tool", false},
		{"/usr/local/bin/apm-tool", false},
		// Path sharing /tmp prefix but not under it.
		{"/tmpfiles/apm-tool", false},
	}
	for _, tc := range cases {
		got := pathUnderTmp(tc.path)
		if got != tc.want {
			t.Errorf("pathUnderTmp(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestPathUnderHome(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/home/alice/apm-tool", true},
		{"/home/alice", true},
		{"/root", true},
		{"/root/config", true},
		{"/run/user/1000/apm-tool", true},
		{"/etc/apm-tool", false},
		{"/var/log/apm-tool", false},
		{"/usr/local/bin/apm-tool", false},
		{"/tmp/apm-tool", false},
		// Paths that share a prefix but are not under home.
		{"/homes/apm-tool", false},
		{"/rootdir/config", false},
	}
	for _, tc := range cases {
		got := pathUnderHome(tc.path)
		if got != tc.want {
			t.Errorf("pathUnderHome(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsSystemd_DoesNotPanic(t *testing.T) {
	// isSystemd() checks for /run/systemd/system; it may return true or false
	// depending on the test environment. We only verify it does not panic.
	_ = isSystemd()
}

func TestFindServicesSystemd_ReturnsSlice(t *testing.T) {
	// findServicesSystemd() reads /etc/systemd/system/hdd-apm-tool*.service.
	// In a clean test environment this returns an empty slice.
	svcs := findServicesSystemd()
	if svcs == nil {
		svcs = []linuxService{}
	}
	// Must not panic.
	_ = svcs
}
