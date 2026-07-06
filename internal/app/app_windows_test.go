// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package app

import (
	"strings"
	"testing"
)

func TestQuoteWindowsArg(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"--apm", "--apm"},
		{"254", "254"},
		{`C:\Tools\hdd-apm-tool.exe`, `C:\Tools\hdd-apm-tool.exe`},
		{`C:\My Logs\apm`, `"C:\My Logs\apm"`},
		// A backslash-escaped quote parses as a literal quote both inside and
		// outside a quoted region, so no wrapping is required without spaces.
		{`with"quote`, `with\"quote`},
	}
	for _, tc := range cases {
		got := quoteWindowsArg(tc.input)
		if got != tc.want {
			t.Errorf("quoteWindowsArg(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestQuoteWindowsArg_NoInjection(t *testing.T) {
	// An argument with an embedded quote must not be able to break out of its
	// quoted region when the elevated process re-parses the command line.
	got := quoteWindowsArg(`C:\x" --apm 1 "y`)
	if strings.Contains(got, `x" --apm`) {
		t.Errorf("embedded quote not escaped: %q", got)
	}
}

func TestIsElevated_DoesNotPanic(t *testing.T) {
	// Result depends on how the test process was launched; only verify that the
	// SID allocation and token membership check complete without panicking.
	_ = isElevated()
}
