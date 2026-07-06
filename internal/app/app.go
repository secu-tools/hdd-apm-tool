// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

// Package app provides the CLI entry point, version banner, disk enumeration,
// APM configuration loop, service management dispatch, and service-mode
// runtime for HDD APM Tool.
package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/secu-tools/hdd-apm-tool/internal/ata"
	"github.com/secu-tools/hdd-apm-tool/internal/logging"
	"github.com/secu-tools/hdd-apm-tool/internal/service"
)

// version, commit, and buildNumber are injected via ldflags at build time:
//
//	-X github.com/secu-tools/hdd-apm-tool/internal/app.version=1.0.0
//	-X github.com/secu-tools/hdd-apm-tool/internal/app.commit=abc1234
//	-X github.com/secu-tools/hdd-apm-tool/internal/app.buildNumber=42
var (
	version     = "1.0.0"
	commit      = "dev"
	buildNumber = "0"
)

func fullVersion() string {
	return fmt.Sprintf("%s.%s", version, buildNumber)
}

// resolveCommitLabel returns the short commit hash, falling back to
// runtime/debug.ReadBuildInfo for go-install builds.
func resolveCommitLabel() string {
	if commit != "dev" && commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return "Go"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return "dev"
}

// resolveVersion returns the display version string.
func resolveVersion() string {
	if commit != "dev" && commit != "" {
		return fullVersion()
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return fullVersion()
}

func versionString() string {
	return fmt.Sprintf(
		"HDD APM Tool - %s (%s)\n"+
			"Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)\n"+
			"Github Repository: https://github.com/secu-tools/hdd-apm-tool",
		resolveVersion(), resolveCommitLabel())
}

func printBanner(level uint8, dryRun bool) {
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("  HDD APM Tool")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("  Version:    %s (%s)\n", resolveVersion(), resolveCommitLabel())
	fmt.Printf("  Platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Target APM: %d", level)
	switch {
	case level == ata.APMDisable:
		fmt.Print(" (DISABLE APM - maximum performance)")
	case level >= 128:
		fmt.Print(" (enabled, no spindown)")
	default:
		fmt.Print(" (enabled, allows spindown)")
	}
	fmt.Println()
	if dryRun {
		fmt.Println("  Mode:       DRY RUN (no changes will be made)")
	}
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println()
}

// RunConfig holds the resolved CLI options for this invocation.
type RunConfig struct {
	APMLevel uint8
	DryRun   bool
	LogDir   string
	SvcName  string
}

// ScanResults holds aggregate counts and per-disk outcomes.
type ScanResults struct {
	Total   int
	Success int
	Skipped int
	Failed  int
	Items   []ata.APMResult
}

// Run is the main entry point called from main.go.
func Run() {
	apmLevel := flag.Int("apm", 255, "APM level (1-254=enable at level, 255=disable APM)")
	dryRun := flag.Bool("dry-run", false, "Show what would be done without making changes")
	showVersion := flag.Bool("version", false, "Show version and exit")
	installSvc := flag.Bool("install", false, "Install as system service")
	uninstallSvc := flag.Bool("uninstall", false, "Uninstall system service")
	logDir := flag.String("logdir", "", "Custom log directory path")
	svcMode := flag.Bool("service", false, "") // internal flag; not shown in help
	svcName := flag.String("svcname", "", "")  // internal flag for Windows SCM name
	flag.Parse()

	// Validate before anything else. The int->uint8 conversions below would
	// otherwise silently truncate out-of-range values (e.g. 300 -> 44), and
	// the Windows SCM handoff must never run with an unvalidated level.
	if *apmLevel < 1 || *apmLevel > 255 {
		fmt.Fprintf(os.Stderr, "ERROR | APM level must be between 1 and 255 (got %d)\n", *apmLevel)
		fmt.Fprintf(os.Stderr, "  1-127:  Enable APM, allow spindown\n")
		fmt.Fprintf(os.Stderr, "  128-254: Enable APM, no spindown\n")
		fmt.Fprintf(os.Stderr, "  255:    Disable APM entirely (default)\n")
		os.Exit(1)
	}

	// When started by the Windows Service Control Manager, hand off immediately.
	if maybeRunAsWindowsService(*svcName, *logDir, uint8(*apmLevel), *dryRun) {
		return
	}

	if *showVersion {
		fmt.Println(versionString())
		os.Exit(0)
	}

	// Must be elevated for all disk operations. On Windows this triggers a UAC
	// dialog; the function never returns (re-launches and exits current process).
	if !isElevated() {
		requestElevation()
	}

	if *installSvc || *uninstallSvc {
		handleService(*installSvc, uint8(*apmLevel), *logDir)
		return
	}

	cfg := RunConfig{
		APMLevel: uint8(*apmLevel),
		DryRun:   *dryRun,
		LogDir:   *logDir,
		SvcName:  *svcName,
	}

	// Logging: write to file only when --logdir is explicitly provided.
	// Without --logdir, output goes to stdout only (services log via their
	// own captured stdout channel, e.g. journald on Linux).
	var logr *logging.Logger
	if *logDir != "" {
		if err := logging.SetLogDir(*logDir); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR | Failed to set log directory: %v\n", err)
			os.Exit(1)
		}
		var ferr error
		logr, ferr = logging.New("hdd-apm-tool.log", logging.DefaultConfig(), "app")
		if ferr != nil {
			logr = logging.NewStdoutOnly(logging.DefaultConfig(), "app")
		}
	} else {
		logr = logging.NewStdoutOnly(logging.DefaultConfig(), "app")
	}
	defer logr.Close()

	if *svcMode {
		// Service mode: apply APM once then wait for a stop signal.
		ctx := serviceContext()
		runWithContext(ctx, cfg, logr)
		return
	}

	// Interactive one-shot mode.
	printBanner(cfg.APMLevel, cfg.DryRun)
	results := processAllDisks(cfg, logr)
	printSummary(cfg, results)
}

// runWithContext applies APM settings once and returns.
// It is a one-shot operation: apply, log, done. The caller (Windows SCM Execute
// or the Linux --service branch) handles any signal/stop logic.
// ctx is checked before starting so that a shutdown arriving during boot-up
// sequencing can still abort the run cleanly.
func runWithContext(ctx context.Context, cfg RunConfig, logr *logging.Logger) {
	select {
	case <-ctx.Done():
		logr.Infof("Service stopping before APM pass (context cancelled)")
		return
	default:
	}
	logr.Infof("Applying APM level %d to all spinning HDD drives", cfg.APMLevel)
	results := processAllDisks(cfg, logr)
	logr.Infof("APM pass complete: %d applied, %d skipped, %d failed",
		results.Success, results.Skipped, results.Failed)
}

func handleService(install bool, apmLevel uint8, logDir string) {
	cfg := service.ServiceConfig{
		APMLevel: apmLevel,
		LogDir:   logDir,
	}
	if install {
		if err := service.Install(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR | Service install failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := service.Uninstall(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR | Service uninstall failed: %v\n", err)
			os.Exit(1)
		}
	}
}

// processAllDisks enumerates drives and applies the configured APM level to
// every eligible HDD. SSDs and NVMe drives are always skipped.
func processAllDisks(cfg RunConfig, logr *logging.Logger) ScanResults {
	paths, err := ata.EnumerateDisks()
	if err != nil {
		logr.Errorf("Failed to enumerate disks: %v", err)
		return ScanResults{}
	}
	if len(paths) == 0 {
		logr.Infof("No disk drives found")
		return ScanResults{}
	}
	logr.Infof("Found %d disk device(s)", len(paths))

	var res ScanResults
	res.Total = len(paths)
	for i, path := range paths {
		result := processSingleDisk(i+1, path, cfg, logr)
		switch {
		case result.Skipped:
			res.Skipped++
		case result.Success:
			res.Success++
		default:
			res.Failed++
		}
		res.Items = append(res.Items, result)
	}
	return res
}

// processSingleDisk evaluates one disk path and returns the APM operation outcome.
func processSingleDisk(num int, path string, cfg RunConfig, logr *logging.Logger) ata.APMResult {
	iface := ata.DetectInterface(path)
	logr.Infof("--- Disk %d: %s [%s] ---", num, path, iface)
	result := ata.APMResult{Disk: ata.DiskInfo{Path: path, Interface: iface}}

	// Skip NVMe drives - they use different power management (NVMe APST).
	// The interface string already comes from platform bus detection, so a
	// separate ata.IsNVMeDevice probe (a second device open + ioctl on
	// Windows) is not needed here.
	if iface == "NVMe" {
		result.Skipped = true
		result.SkipReason = "NVMe drive - APM is not applicable (NVMe uses NVMe APST)"
		logr.Infof("  Skip: NVMe device")
		return result
	}

	rawIdentify, err := ata.IdentifyDevice(path)
	if err != nil {
		result.Skipped = true
		result.SkipReason = fmt.Sprintf("Cannot identify device: %v", err)
		logr.Warnf("  Skip: cannot identify (%v)", err)
		return result
	}

	id := ata.ParseIdentify(rawIdentify)
	result.Disk.Model = id.Model()
	result.Disk.Serial = id.SerialNumber()
	result.Disk.Firmware = id.FirmwareRevision()
	result.Disk.IsSSDDrive = id.IsSSD()
	result.Disk.RotationRPM = id.RotationRate()
	logr.Infof("  Model: %s | Firmware: %s", result.Disk.Model, result.Disk.Firmware)

	// Skip SSD drives - APM spindown settings are meaningless for solid-state
	// devices and some SSDs behave incorrectly when APM is forcibly set.
	if id.IsSSD() {
		result.Skipped = true
		result.SkipReason = "SSD - APM spindown not applicable to solid-state devices"
		logr.Infof("  Type: SSD - skipped")
		return result
	}

	if id.RotationRate() > 1 {
		logr.Infof("  Type: HDD (%d RPM)", id.RotationRate())
	} else {
		logr.Infof("  Type: HDD (rotation rate not reported)")
	}

	if !id.IsAPMSupported() {
		result.Skipped = true
		result.Disk.APMSupported = false
		result.SkipReason = categorizeNoAPMReason(result.Disk.Model)
		logr.Infof("  APM support: NO - %s", result.SkipReason)
		return result
	}

	result.Disk.APMSupported = true
	logr.Infof("  APM support: YES")

	if id.IsAPMEnabled() {
		result.Disk.APMEnabled = true
		result.Disk.APMLevel = id.CurrentAPMLevel()
		result.WasEnabled = true
		result.OldLevel = id.CurrentAPMLevel()
		logr.Infof("  Current APM: ENABLED (level %d / 0x%02X)", result.OldLevel, result.OldLevel)
	} else {
		logr.Infof("  Current APM: DISABLED")
	}

	needChange := (cfg.APMLevel == ata.APMDisable && id.IsAPMEnabled()) ||
		(cfg.APMLevel != ata.APMDisable && (!id.IsAPMEnabled() || id.CurrentAPMLevel() != cfg.APMLevel))

	if !needChange {
		result.Success = true
		result.Skipped = true
		result.SkipReason = "Already at desired APM setting"
		logr.Infof("  Action: no change needed")
		return result
	}

	result.NewLevel = cfg.APMLevel
	if cfg.DryRun {
		logr.Infof("  Action: WOULD SET APM to %d (dry run)", cfg.APMLevel)
		result.Success = true
		return result
	}

	logr.Infof("  Action: setting APM to %d", cfg.APMLevel)
	if err := ata.SetAPM(path, cfg.APMLevel); err != nil {
		result.Error = err
		logr.Errorf("  Result: FAILED - %v", err)
		return result
	}
	result.Success = true
	logr.Infof("  Result: SUCCESS")
	return result
}

// categorizeNoAPMReason provides a human-readable explanation for why a
// drive does not support ATA APM.
func categorizeNoAPMReason(model string) string {
	mu := strings.ToUpper(model)
	if strings.Contains(mu, "SAMSUNG") {
		return "Samsung drives do not implement ATA APM (uses internal power management)"
	}
	if strings.Contains(mu, "NVME") {
		return "NVMe drives use NVMe APST, not ATA APM"
	}
	return "Drive firmware does not report APM capability in IDENTIFY DEVICE response"
}

// diskDisplayName returns a short label for a disk: model string when known,
// device path otherwise.
func diskDisplayName(d ata.DiskInfo) string {
	if d.Model != "" {
		return d.Model
	}
	return d.Path
}

func printSkippedSection(items []ata.APMResult) {
	fmt.Println("  Skipped drives:")
	for _, r := range items {
		if r.Skipped && r.SkipReason != "" {
			fmt.Printf("    - %-30s : %s\n", truncate(diskDisplayName(r.Disk), 30), r.SkipReason)
		}
	}
	fmt.Println()
}

func printFailedSection(items []ata.APMResult) {
	fmt.Println("  Failed drives:")
	for _, r := range items {
		if !r.Success && !r.Skipped && r.Error != nil {
			fmt.Printf("    - %-30s : %v\n", truncate(diskDisplayName(r.Disk), 30), r.Error)
		}
	}
	fmt.Println()
}

func printSummary(cfg RunConfig, res ScanResults) {
	sep := strings.Repeat("=", 70)
	dash := strings.Repeat("-", 70)
	fmt.Println(sep)
	fmt.Println("  SUMMARY")
	fmt.Println(sep)
	fmt.Printf("  Total disks scanned: %d\n", res.Total)
	fmt.Printf("  Successfully set:    %d\n", res.Success)
	fmt.Printf("  Skipped:             %d\n", res.Skipped)
	fmt.Printf("  Failed:              %d\n", res.Failed)
	fmt.Println()

	if res.Skipped > 0 {
		printSkippedSection(res.Items)
	}
	if res.Failed > 0 {
		printFailedSection(res.Items)
	}

	fmt.Println(dash)
	fmt.Println("  IMPORTANT: APM settings are stored in volatile memory on most drives.")
	fmt.Println("  They reset to drive defaults after a power cycle or reboot.")
	fmt.Println()
	fmt.Println("  To apply APM settings persistently at every boot:")
	if runtime.GOOS == "windows" {
		fmt.Printf("    hdd-apm-tool --install --apm %d\n", cfg.APMLevel)
	} else {
		fmt.Printf("    sudo hdd-apm-tool --install --apm %d\n", cfg.APMLevel)
	}
	fmt.Println(dash)
	fmt.Println()

	if res.Failed > 0 {
		os.Exit(1)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
