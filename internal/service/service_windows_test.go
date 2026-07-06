// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package service

import (
	"strings"
	"testing"
)

func TestSystem32Path(t *testing.T) {
	got := scExePath()
	if !strings.HasSuffix(got, `\System32\sc.exe`) {
		t.Errorf("scExePath() = %q, want path ending in \\System32\\sc.exe", got)
	}
	if !strings.Contains(got, `:\`) {
		t.Errorf("scExePath() = %q, want an absolute path", got)
	}
	ps := powershellPath()
	if !strings.HasSuffix(ps, `\WindowsPowerShell\v1.0\powershell.exe`) {
		t.Errorf("powershellPath() = %q, want the System32 PowerShell path", ps)
	}
}

func TestBuildServiceBinPath_PlainArgs(t *testing.T) {
	got := buildServiceBinPath(`C:\Tools\hdd-apm-tool.exe`, []string{"--apm", "254", "--service"})
	want := `C:\Tools\hdd-apm-tool.exe --apm 254 --service`
	if got != want {
		t.Errorf("buildServiceBinPath() = %q, want %q", got, want)
	}
}

func TestBuildServiceBinPath_ExeWithSpaces(t *testing.T) {
	got := buildServiceBinPath(`C:\Program Files\HDD APM\hdd-apm-tool.exe`, []string{"--apm", "255"})
	want := `"C:\Program Files\HDD APM\hdd-apm-tool.exe" --apm 255`
	if got != want {
		t.Errorf("buildServiceBinPath() = %q, want %q", got, want)
	}
}

func TestBuildServiceBinPath_LogDirWithSpaces(t *testing.T) {
	got := buildServiceBinPath(`C:\t.exe`, []string{"--logdir", `C:\My Logs\apm`})
	want := `C:\t.exe --logdir "C:\My Logs\apm"`
	if got != want {
		t.Errorf("buildServiceBinPath() = %q, want %q", got, want)
	}
}

func TestBuildServiceBinPath_QuoteInjection(t *testing.T) {
	// An argument containing a double quote must not be able to terminate the
	// quoted region and smuggle extra arguments into the service command line.
	got := buildServiceBinPath(`C:\t.exe`, []string{"--logdir", `C:\x" --apm 1 --y "z`})
	if strings.Contains(got, `x" --apm 1`) {
		t.Errorf("embedded quote not escaped in binPath: %q", got)
	}
}

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
