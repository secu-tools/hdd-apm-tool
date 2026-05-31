// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build linux

package service

import "testing"

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
