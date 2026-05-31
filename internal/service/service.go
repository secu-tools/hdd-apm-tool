// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

// Package service provides cross-platform system service installation and
// uninstallation for APM Tool (Windows, Linux).
//
// On Windows the tool is registered as a Windows Service via sc.exe.
// On Linux it is registered as a systemd unit.
//
// During install the user is prompted for an optional service label so that
// multiple instances (e.g. different APM levels for different machines) can
// coexist. The --logdir flag is preserved in the service command line when
// it was specified at install time.
package service

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ServiceConfig holds parameters for service installation.
type ServiceConfig struct {
	APMLevel     uint8  // APM level to apply (1-254=enable, 255=disable)
	LogDir       string // Custom log directory (empty = platform default)
	ExePath      string // Absolute path to the binary (auto-detected when empty)
	DisplayLabel string // User-chosen label for multi-instance support
}

// ServiceName returns the service identifier used by the OS.
func (sc *ServiceConfig) ServiceName() string {
	if sc.DisplayLabel != "" {
		return "hdd-apm-tool_" + sanitizeServiceLabel(sc.DisplayLabel)
	}
	return "hdd-apm-tool"
}

// DisplayName returns a human-readable service display name.
func (sc *ServiceConfig) DisplayName() string {
	if sc.DisplayLabel != "" {
		return "HDD APM Tool (" + sc.DisplayLabel + ")"
	}
	return "HDD APM Tool"
}

// ServerArgs returns the command-line arguments to embed in the service entry.
func (sc *ServiceConfig) ServerArgs() []string {
	var args []string
	args = append(args, "--apm", fmt.Sprintf("%d", sc.APMLevel))
	args = append(args, "--service")
	if sc.LogDir != "" {
		args = append(args, "--logdir", sc.LogDir)
	}
	return args
}

// resolveExePath finds the absolute path of the running binary.
func (sc *ServiceConfig) resolveExePath() (string, error) {
	if sc.ExePath != "" {
		return sc.ExePath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("detect executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return exe, nil
}

var safeLabel = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeServiceLabel removes characters that are unsafe for service names.
func sanitizeServiceLabel(label string) string {
	label = strings.TrimSpace(label)
	label = safeLabel.ReplaceAllString(label, "_")
	if label == "" {
		return "default"
	}
	return label
}

// Install registers APM Tool as a system service and prompts the user for an
// optional label.
func Install(cfg ServiceConfig) error {
	if err := fillDefaults(&cfg); err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Service Installation")
	fmt.Printf("  APM level to apply at startup: %d\n", cfg.APMLevel)
	fmt.Println("  The service will be named \"HDD APM Tool\".")
	fmt.Print("  Enter a custom label (or press Enter to skip): ")
	label := strings.TrimSpace(readLine(reader))
	if label != "" {
		cfg.DisplayLabel = label
	}
	fmt.Printf("  Service name: %s\n\n", cfg.DisplayName())
	return platformInstall(cfg)
}

// Uninstall removes an APM Tool system service. Lists all installed instances
// and prompts the user to pick one.
func Uninstall(cfg ServiceConfig) error {
	if err := fillDefaults(&cfg); err != nil {
		return err
	}
	return platformUninstall(cfg)
}

// platformInstall and platformUninstall are set by the platform-specific files.
var platformInstall func(ServiceConfig) error
var platformUninstall func(ServiceConfig) error

func fillDefaults(cfg *ServiceConfig) error {
	if cfg.ExePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("detect executable path: %w", err)
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}
		cfg.ExePath = exe
	}
	return nil
}

func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "  Warning: failed to read input: %v\n", err)
	}
	return strings.TrimSpace(line)
}
