// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

package service

import (
	"fmt"
	"strings"
	"testing"
)

// =====================================================================
// ServiceConfig.ServiceName
// =====================================================================

func TestServiceName_Default(t *testing.T) {
	cfg := &ServiceConfig{}
	if got := cfg.ServiceName(); got != "hdd-apm-tool" {
		t.Errorf("ServiceName() = %q, want %q", got, "hdd-apm-tool")
	}
}

func TestServiceName_WithLabel(t *testing.T) {
	cfg := &ServiceConfig{DisplayLabel: "home"}
	want := "hdd-apm-tool_home"
	if got := cfg.ServiceName(); got != want {
		t.Errorf("ServiceName() = %q, want %q", got, want)
	}
}

func TestServiceName_LabelWithSpaces(t *testing.T) {
	cfg := &ServiceConfig{DisplayLabel: "my server"}
	got := cfg.ServiceName()
	if strings.Contains(got, " ") {
		t.Errorf("ServiceName() should not contain spaces, got %q", got)
	}
}

// =====================================================================
// ServiceConfig.DisplayName
// =====================================================================

func TestDisplayName_Default(t *testing.T) {
	cfg := &ServiceConfig{}
	want := "HDD APM Tool"
	if got := cfg.DisplayName(); got != want {
		t.Errorf("DisplayName() = %q, want %q", got, want)
	}
}

func TestDisplayName_WithLabel(t *testing.T) {
	cfg := &ServiceConfig{DisplayLabel: "office"}
	want := "HDD APM Tool (office)"
	if got := cfg.DisplayName(); got != want {
		t.Errorf("DisplayName() = %q, want %q", got, want)
	}
}

// =====================================================================
// ServiceConfig.ServerArgs
// =====================================================================

func TestServerArgs_ContainsAPMLevel(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 128}
	args := cfg.ServerArgs()
	found := false
	for i, a := range args {
		if a == "--apm" && i+1 < len(args) && args[i+1] == "128" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --apm 128", args)
	}
}

func TestServerArgs_ContainsServiceFlag(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 254}
	args := cfg.ServerArgs()
	found := false
	for _, a := range args {
		if a == "--service" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --service flag", args)
	}
}

func TestServerArgs_WithLogDir(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 254, LogDir: "/var/log/apm-tool"}
	args := cfg.ServerArgs()
	found := false
	for i, a := range args {
		if a == "--logdir" && i+1 < len(args) && args[i+1] == "/var/log/apm-tool" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --logdir /var/log/apm-tool", args)
	}
}

func TestServerArgs_NoLogDir(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 128}
	args := cfg.ServerArgs()
	for _, a := range args {
		if a == "--logdir" {
			t.Errorf("ServerArgs() should not contain --logdir when LogDir is empty, got %v", args)
		}
	}
}

func TestServerArgs_DisableAPM(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 255}
	args := cfg.ServerArgs()
	found := false
	for i, a := range args {
		if a == "--apm" && i+1 < len(args) && args[i+1] == "255" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --apm 255", args)
	}
}

// =====================================================================
// sanitizeServiceLabel
// =====================================================================

func TestSanitizeServiceLabel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"home", "home"},
		{"my-server", "my-server"},
		{"my_server_01", "my_server_01"},
		{"has spaces", "has_spaces"},
		{"special!@#$", "special____"},
		{"", "default"},
		{"  trimmed  ", "trimmed"},
		{"abc123", "abc123"},
		{"a-b_c", "a-b_c"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeServiceLabel(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeServiceLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// =====================================================================
// ServerArgs - APM level edge cases
// =====================================================================

func TestServerArgs_MinimumAPMLevel(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 1}
	args := cfg.ServerArgs()
	found := false
	for i, a := range args {
		if a == "--apm" && i+1 < len(args) && args[i+1] == "1" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --apm 1", args)
	}
}

func TestServerArgs_MaximumAPMLevel(t *testing.T) {
	cfg := &ServiceConfig{APMLevel: 254}
	args := cfg.ServerArgs()
	found := false
	for i, a := range args {
		if a == "--apm" && i+1 < len(args) && args[i+1] == "254" {
			found = true
		}
	}
	if !found {
		t.Errorf("ServerArgs() = %v, want --apm 254", args)
	}
}

// =====================================================================
// ServiceConfig APM level round-trip via ServerArgs
// =====================================================================

func TestServiceConfig_APMLevelPreserved(t *testing.T) {
	levels := []uint8{1, 64, 127, 128, 200, 254, 255}
	for _, level := range levels {
		cfg := &ServiceConfig{APMLevel: level}
		args := cfg.ServerArgs()
		want := fmt.Sprintf("%d", level)
		found := false
		for i, a := range args {
			if a == "--apm" && i+1 < len(args) && args[i+1] == want {
				found = true
			}
		}
		if !found {
			t.Errorf("APM level %d not found in ServerArgs: %v", level, args)
		}
	}
}
