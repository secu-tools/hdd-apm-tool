// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build linux

package service

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func init() {
	platformInstall = installLinux
	platformUninstall = uninstallLinux
}

func installLinux(cfg ServiceConfig) error {
	if isSystemd() {
		return installSystemd(cfg)
	}
	return fmt.Errorf("unsupported init system: only systemd is supported on Linux")
}

func uninstallLinux(cfg ServiceConfig) error {
	if isSystemd() {
		return uninstallSystemd(cfg)
	}
	return fmt.Errorf("unsupported init system: only systemd is supported on Linux")
}

func isSystemd() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func installSystemd(cfg ServiceConfig) error {
	exe, err := cfg.resolveExePath()
	if err != nil {
		return err
	}

	name := cfg.ServiceName()
	unitPath := "/etc/systemd/system/" + name + ".service"

	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("service %q already exists at %s; use --uninstall first or choose a different label", name, unitPath)
	}

	args := cfg.ServerArgs()
	execStart := exe
	if len(args) > 0 {
		execStart += " " + strings.Join(args, " ")
	}

	// Relax security settings when the binary or log directory is under
	// a path that systemd's stricter namespacing would break.
	protectHome := "yes"
	if pathUnderHome(exe) || (cfg.LogDir != "" && pathUnderHome(cfg.LogDir)) {
		protectHome = "no"
	}

	privateTmp := "true"
	if pathUnderTmp(exe) || (cfg.LogDir != "" && pathUnderTmp(cfg.LogDir)) {
		privateTmp = "false"
	}

	protectSystem := "strict"
	if pathUnderTmp(exe) || (cfg.LogDir != "" && pathUnderTmp(cfg.LogDir)) {
		protectSystem = "no"
	}

	// When --logdir was supplied at install time, create the directory before
	// starting and grant write access inside the stricter ProtectSystem sandbox.
	var execStartPre, readWritePaths string
	if cfg.LogDir != "" {
		execStartPre = "ExecStartPre=+/bin/mkdir -p " + cfg.LogDir + "\n"
		readWritePaths = "ReadWritePaths=" + cfg.LogDir + "\n"
	}

	// Type=oneshot: systemd waits for the process to exit before marking the
	// unit as started. RemainAfterExit=yes means the unit shows "active
	// (exited)" after the process finishes, so it does not look like a failure.
	// No Restart= is set because this is an intentional run-once-at-boot tool;
	// systemd's start=auto causes it to run again on the next boot.
	unit := fmt.Sprintf(`[Unit]
Description=%s
After=local-fs.target network.target
Wants=local-fs.target

[Service]
Type=oneshot
RemainAfterExit=yes
%sExecStart=%s
LimitNOFILE=1024

# Security hardening
ProtectSystem=%s
%sNoNewPrivileges=yes
PrivateTmp=%s
ProtectHome=%s

# Drive access requires raw block device capabilities
AmbientCapabilities=CAP_SYS_RAWIO CAP_DAC_READ_SEARCH

[Install]
WantedBy=multi-user.target
`, cfg.DisplayName(), execStartPre, execStart, protectSystem, readWritePaths, privateTmp, protectHome)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file %s: %w", unitPath, err)
	}

	for _, c := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", name},
		{"systemctl", "start", name},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s (%w)", strings.Join(c, " "), strings.TrimSpace(string(out)), err)
		}
	}

	fmt.Printf("Service %q (%s) installed and started.\n", cfg.DisplayName(), name)
	fmt.Printf("  Unit file: %s\n", unitPath)
	fmt.Printf("  Status:    systemctl status %s\n", name)
	fmt.Printf("  Logs:      journalctl -u %s -f\n", name)
	fmt.Printf("  Remove:    hdd-apm-tool --uninstall\n")
	return nil
}

func uninstallSystemd(cfg ServiceConfig) error {
	services := findServicesSystemd()
	if len(services) == 0 {
		fmt.Println("No hdd-apm-tool services found.")
		return nil
	}

	fmt.Println("Found hdd-apm-tool services:")
	fmt.Println()
	for i, svc := range services {
		fmt.Printf("  %d. %s\n", i+1, svc.name)
		fmt.Printf("     Command: %s\n", svc.execStart)
		fmt.Println()
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the number to uninstall (or press Enter to cancel): ")
	choice := strings.TrimSpace(readLineLinux(reader))
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
	idx--
	if idx < 0 || idx >= len(services) {
		fmt.Println("Invalid choice.")
		return nil
	}

	svc := services[idx]
	exec.Command("systemctl", "stop", svc.name).Run()
	exec.Command("systemctl", "disable", svc.name).Run()

	unitPath := "/etc/systemd/system/" + svc.name + ".service"
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file %s: %w", unitPath, err)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		fmt.Printf("Warning: daemon-reload failed: %s\n", strings.TrimSpace(string(out)))
	}

	fmt.Printf("Service %q uninstalled.\n", svc.name)
	return nil
}

type linuxService struct {
	name      string
	execStart string
}

// findServicesSystemd lists systemd units whose ExecStart path contains "hdd-apm-tool".
func findServicesSystemd() []linuxService {
	pattern := "/etc/systemd/system/hdd-apm-tool*.service"
	files, _ := filepath.Glob(pattern)

	var services []linuxService
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f), ".service")
		execStart := ""
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ExecStart=") {
				execStart = strings.TrimPrefix(line, "ExecStart=")
				break
			}
		}
		services = append(services, linuxService{name: name, execStart: execStart})
	}
	return services
}

// pathUnderTmp reports whether path is located under /tmp.
func pathUnderTmp(path string) bool {
	return path == "/tmp" || strings.HasPrefix(path, "/tmp/")
}

// pathUnderHome reports whether path is located under common home directories.
func pathUnderHome(path string) bool {
	prefixes := []string{"/home/", "/root/", "/run/user/"}
	for _, p := range prefixes {
		if path == strings.TrimSuffix(p, "/") || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func readLineLinux(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}
