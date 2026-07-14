// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build darwin

// Package service -- macOS implementation using launchd.
// HDD APM Tool is a run-once-at-boot tool: RunAtLoad=true, KeepAlive=false.
// The daemon plist is installed to /Library/LaunchDaemons/ (root-owned,
// loaded by launchd at boot for all users).
package service

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func init() {
	platformInstall = installDarwin
	platformUninstall = uninstallDarwin
}

// darwinLabelPrefix is the reverse-DNS domain used for launchd plist labels.
const darwinLabelPrefix = "com.secu-tools."

// darwinDefaultLogDir is the default directory for launchd stdout/stderr logs.
const darwinDefaultLogDir = "/var/log/hdd-apm-tool"

// darwinLaunchDaemonsDir is where system-wide launchd plists are stored.
const darwinLaunchDaemonsDir = "/Library/LaunchDaemons"

// installDarwin registers HDD APM Tool as a launchd daemon.
func installDarwin(cfg ServiceConfig) error {
	exe, err := cfg.resolveExePath()
	if err != nil {
		return err
	}

	name := cfg.ServiceName()
	label := darwinLabelPrefix + name
	plistPath := filepath.Join(darwinLaunchDaemonsDir, label+".plist")

	if _, err := os.Stat(plistPath); err == nil {
		return fmt.Errorf("service %q already exists at %s\n"+
			"Use 'hdd-apm-tool --uninstall' to remove it first, or choose a different label.",
			label, plistPath)
	}

	logDir := darwinDefaultLogDir
	if cfg.LogDir != "" {
		logDir = cfg.LogDir
	}

	plist := buildDarwinPlist(cfg, exe, logDir, label)

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log directory %s: %w", logDir, err)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist %s: %w", plistPath, err)
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	fmt.Printf("Service %q (%s) installed and loaded.\n", cfg.DisplayName(), label)
	fmt.Printf("  Status:  launchctl list | grep %s\n", label)
	fmt.Printf("  Logs:    tail -f %s/%s.stdout.log\n", logDir, name)
	fmt.Printf("  Stop:    launchctl unload %s\n", plistPath)
	fmt.Printf("  Remove:  hdd-apm-tool --uninstall\n")
	return nil
}

// uninstallDarwin removes an HDD APM Tool launchd daemon.
func uninstallDarwin(cfg ServiceConfig) error {
	services := findServicesHddApmToolDarwin()
	if len(services) == 0 {
		fmt.Println("No hdd-apm-tool launchd services found.")
		return nil
	}

	fmt.Println("Found hdd-apm-tool services:")
	fmt.Println()
	for i, svc := range services {
		fmt.Printf("  %d. %s\n", i+1, svc.label)
		fmt.Printf("     Plist: %s\n", svc.plistPath)
		fmt.Println()
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the number to uninstall (or press Enter to cancel): ")
	choice := strings.TrimSpace(readLineDarwin(reader))
	if choice == "" {
		fmt.Println("Cancelled.")
		return nil
	}

	idx := 0
	for _, c := range choice {
		if c < '0' || c > '9' {
			fmt.Println("Invalid choice.")
			return nil
		}
		idx = idx*10 + int(c-'0')
	}
	idx-- // convert from 1-based to 0-based
	if idx < 0 || idx >= len(services) {
		fmt.Println("Invalid choice.")
		return nil
	}

	svc := services[idx]
	exec.Command("launchctl", "unload", svc.plistPath).Run() //nolint:errcheck

	if err := os.Remove(svc.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist %s: %w", svc.plistPath, err)
	}

	fmt.Printf("Service %q uninstalled.\n", svc.label)
	return nil
}

// darwinService holds the details of an installed launchd service.
type darwinService struct {
	label     string
	plistPath string
}

// findServicesHddApmToolDarwin scans /Library/LaunchDaemons for plists that
// match the hdd-apm-tool label pattern.
func findServicesHddApmToolDarwin() []darwinService {
	entries, err := os.ReadDir(darwinLaunchDaemonsDir)
	if err != nil {
		return nil
	}
	var services []darwinService
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.Contains(name, "hdd-apm-tool") || !strings.HasSuffix(name, ".plist") {
			continue
		}
		services = append(services, darwinService{
			label:     strings.TrimSuffix(name, ".plist"),
			plistPath: filepath.Join(darwinLaunchDaemonsDir, name),
		})
	}
	return services
}

// buildDarwinPlist generates the launchd plist XML for the service.
// Exported as a separate function so it can be unit-tested without root access.
func buildDarwinPlist(cfg ServiceConfig, exe, logDir, label string) string {
	name := cfg.ServiceName()
	args := cfg.ServerArgs()

	var argElements string
	for _, a := range args {
		argElements += "        <string>" + xmlEscapeAttr(a) + "</string>\n"
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
%s    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>%s/%s.stdout.log</string>
    <key>StandardErrorPath</key>
    <string>%s/%s.stderr.log</string>
</dict>
</plist>
`, label, xmlEscapeAttr(exe), argElements, logDir, name, logDir, name)
}

// xmlEscapeAttr escapes the five XML special characters for safe embedding
// inside a plist <string> value.
func xmlEscapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

func readLineDarwin(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "  Warning: failed to read input: %v\n", err)
	}
	return strings.TrimSpace(line)
}
