// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package service

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func init() {
	platformInstall = installWindows
	platformUninstall = uninstallWindows
}

func installWindows(cfg ServiceConfig) error {
	exe, err := cfg.resolveExePath()
	if err != nil {
		return err
	}

	name := cfg.ServiceName()

	// Fail early if the service already exists.
	if out, err := exec.Command("sc.exe", "query", name).Output(); err == nil {
		if strings.Contains(string(out), "SERVICE_NAME") {
			return fmt.Errorf("service %q already exists; use --uninstall to remove it first or choose a different label", name)
		}
	}

	args := cfg.ServerArgs()
	// Embed --svcname so the binary knows its SCM name when launched by the SCM.
	svcArgs := append([]string{"--svcname", name}, args...)
	binPath := `"` + exe + `" ` + strings.Join(svcArgs, " ")

	out, err := exec.Command("sc.exe", "create", name,
		"binPath=", binPath,
		"start=", "auto",
		"DisplayName=", cfg.DisplayName(),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc create: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	desc := "HDD APM Tool - Applies ATA Advanced Power Management settings to HDD drives once."
	if out, err := exec.Command("sc.exe", "description", name, desc).CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to set service description: %s\n", strings.TrimSpace(string(out)))
	}

	if out, err := exec.Command("sc.exe", "failure", name,
		"reset=", "86400",
		"actions=", "restart/10000/restart/30000/restart/60000",
	).CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to set failure actions: %s\n", strings.TrimSpace(string(out)))
	}

	out, err = exec.Command("sc.exe", "start", name).CombinedOutput()
	if err != nil {
		fmt.Printf("Service created but failed to start: %s\n", strings.TrimSpace(string(out)))
		fmt.Println("You can start it manually: sc start", name)
		return nil
	}

	fmt.Printf("Service %q (%s) installed and started.\n", cfg.DisplayName(), name)
	fmt.Printf("  Status:  sc query %s\n", name)
	fmt.Printf("  Stop:    sc stop %s\n", name)
	fmt.Printf("  Remove:  hdd-apm-tool --uninstall\n")
	return nil
}

func uninstallWindows(cfg ServiceConfig) error {
	services := findServicesWindows()
	if len(services) == 0 {
		fmt.Println("No hdd-apm-tool services found.")
		return nil
	}

	fmt.Println("Found hdd-apm-tool services:")
	fmt.Println()
	for i, svc := range services {
		fmt.Printf("  %d. %s\n", i+1, svc.displayName)
		fmt.Printf("     Command: %s\n", svc.binPath)
		fmt.Println()
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the number to uninstall (or press Enter to cancel): ")
	choice := strings.TrimSpace(readLineStdio(reader))
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
	exec.Command("sc.exe", "stop", svc.name).Run()

	out, err := exec.Command("sc.exe", "delete", svc.name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc delete: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	fmt.Printf("Service %q uninstalled.\n", svc.displayName)
	return nil
}

type winService struct {
	name        string
	displayName string
	binPath     string
}

// findServicesWindows queries the Windows SCM for services whose binary path
// contains "hdd-apm-tool".
func findServicesWindows() []winService {
	psCmd := `Get-WmiObject Win32_Service | Where-Object { $_.PathName -like '*hdd-apm-tool*' } | ForEach-Object { $_.Name + '|' + $_.DisplayName + '|' + $_.PathName }`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", psCmd).Output()
	if err != nil {
		return nil
	}
	var services []winService
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) == 3 {
			services = append(services, winService{
				name:        strings.TrimSpace(parts[0]),
				displayName: strings.TrimSpace(parts[1]),
				binPath:     strings.TrimSpace(parts[2]),
			})
		}
	}
	return services
}

func readLineStdio(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}
