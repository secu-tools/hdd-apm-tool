// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package service

import "testing"

func TestFindServicesWindows_ReturnsSlice(t *testing.T) {
	// findServicesWindows() queries WMI via PowerShell. In environments without
	// apm-tool installed it should return an empty (not nil) slice or nil, and
	// must not panic.
	svcs := findServicesWindows()
	_ = svcs
}

func TestInstallWindows_MissingExe(t *testing.T) {
	// Provide a clearly non-existent path to force an early failure path.
	cfg := ServiceConfig{
		APMLevel: 254,
		ExePath:  `C:\does\not\exist\hdd-apm-tool.exe`,
	}
	// installWindows uses sc.exe, which will fail for a non-existent service
	// check. The test verifies no panic occurs.
	_ = installWindows(cfg)
}

func TestUninstallWindows_NoServices(t *testing.T) {
	// When no services are found, uninstallWindows should print a message and
	// return nil without panicking.
	cfg := ServiceConfig{APMLevel: 254}
	// This may return an error if sc.exe is unavailable, but must not panic.
	_ = uninstallWindows(cfg)
}
