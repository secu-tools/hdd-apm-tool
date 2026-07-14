# HDD APM Tool

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![CI](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/ci.yml/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/ci.yml)
[![Build](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/build.yml/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/build.yml)
[![CodeQL](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/github-code-scanning/codeql)
[![Dependency Graph](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/dependabot/update-graph/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/dependabot/update-graph)

ATA Advanced Power Management (APM) configuration tool for spinning HDDs on
Windows, Linux, and macOS. Issues raw ATA commands directly via OS APIs - no
hdparm, no smartctl, no other external dependencies.

## How It Works

1. Enumerates all disk devices
2. Identifies each drive with ATA IDENTIFY DEVICE
3. Skips NVMe drives (they use NVMe APST, not ATA APM), SSDs (spindown does
   not apply), and drives that do not report APM support
4. Applies the requested level with ATA SET FEATURES

USB-attached drives are supported on all three platforms via SCSI/ATA
Translation (SAT) pass-through; see [Platform Notes](#platform-notes).

The tool is one-shot: it applies APM to every eligible drive and exits.

## APM Levels

| Level | Meaning |
|-------|---------|
| 1 - 127 | APM enabled, spindown allowed (aggressive power saving) |
| 128 - 254 | APM enabled, no spindown (254 = maximum performance) |
| 255 | Disable APM entirely (default) |

## Quick Start

```bash
# Build
go build -o hdd-apm-tool ./

# Dry run - show what would be done
sudo ./hdd-apm-tool --apm 128 --dry-run

# Apply APM level 128 to all HDDs
sudo ./hdd-apm-tool --apm 128

# Disable APM
sudo ./hdd-apm-tool --apm 255
```

> [!NOTE]
> Root/Administrator privileges are required. On Windows the tool prompts for
> UAC elevation automatically; on Linux/macOS run with `sudo`.

> [!TIP]
> Use `--dry-run` first to see which drives would be touched and their
> current APM state.

## Service Installation

> [!WARNING]
> APM settings are volatile: drives reset to their factory defaults after a
> power cycle. Install the tool as a system service to reapply the level
> automatically at every boot.

```bash
# Linux / macOS (requires root)
sudo ./hdd-apm-tool --install --apm 128

# Windows (elevated, or the UAC prompt will appear)
hdd-apm-tool.exe --install --apm 128

# Uninstall
sudo ./hdd-apm-tool --uninstall
hdd-apm-tool.exe --uninstall
```

The service runs once at each boot, applies APM to all eligible drives, and
exits:

- **Linux**: systemd unit (`oneshot`) under `/etc/systemd/system/`
- **macOS**: launchd plist under `/Library/LaunchDaemons/`, logs to
  `/var/log/hdd-apm-tool/`
- **Windows**: auto-start service registered via `sc.exe`, with automatic
  retries in case disk drivers are not ready at first start

> [!NOTE]
> A completed run shows as `active (exited)` in `systemctl status` and as
> `Stopped` in the Windows service list. This is normal for a run-once
> service, not a failure.

> [!TIP]
> The installer prompts for an optional label, so multiple instances with
> different APM levels can coexist on the same machine. After installing, it
> prints the status, log, and uninstall commands for your platform.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--apm` | 255 | APM level to apply (1-254 enable, 255 disable) |
| `--dry-run` | false | Show what would be done without changes |
| `--install` | - | Install as system service |
| `--uninstall` | - | Uninstall system service |
| `--logdir` | (none) | Write log file to this directory in addition to stdout |
| `--version` | - | Print version and exit |

## Logging

Output goes to stdout. `--logdir <dir>` also writes a rotating log file to
that directory. When supplied together with `--install`, the directory is
preserved in the service arguments and used on every boot.

## Building

All platforms use the same flags. Run the script for your OS:

| Platform | Command |
|----------|---------|
| Linux / macOS | `./build.sh [flags]` |
| Windows (PowerShell) | `.\build.ps1 [flags]` |
| Windows (CMD) | `build.cmd [flags]` (forwards to `build.ps1`) |

| Flag | Effect |
|------|--------|
| `-windows` / `-linux` / `-darwin` | Select platform(s); combine freely |
| `-amd64` / `-arm64` | Select architecture(s); combine freely |
| `-all` | Build every platform/arch combination |
| `-deb` / `-rpm` | Package linux builds as .deb/.rpm (combine with `-linux`) |
| `-test` | Run unit tests |
| `-coverage` | Run tests with coverage report |
| `-clean` | Remove build artifacts |

Example: `./build.sh -linux -arm64` builds linux/arm64 only.

> [!NOTE]
> With no flags, `build.sh` builds windows+linux for both amd64 and arm64;
> `build.ps1`/`build.cmd` builds windows+linux for amd64 only. Darwin is
> always opt-in (`-darwin` or `-all`).

All builds are pure Go (`CGO_ENABLED=0`), so any platform can cross-compile
for any other - no Xcode or native toolchain needed.

## Platform Notes

### Linux

Enumerates `/dev/sd*` and `/dev/hd*` devices. Uses HDIO ioctls for direct
SATA drives, with SG_IO SAT16 pass-through fallback for USB-attached drives.
The service logs to the journal: `journalctl -u hdd-apm-tool`.

### Windows

Enumerates `\\.\PhysicalDriveN` devices. Tries three pass-through methods in
order: `IOCTL_ATA_PASS_THROUGH` (direct SATA/AHCI), `SCSI_PASS_THROUGH` +
SAT12 CDB (most SATA/USB bridges), then `SCSI_PASS_THROUGH_DIRECT` + SAT16
CDB (USB Mass Storage bridges).

### macOS

Enumerates disks with `diskutil` and issues ATA commands via the
`DKIOCIOCSCSICOMMAND` ioctl (SAT16 pass-through on `/dev/rdiskN`).

> [!NOTE]
> Internal Mac storage (NVMe/SSD) is enumerated and skipped automatically -
> APM does not apply to it. External spinning HDDs over USB or Thunderbolt
> are fully supported.

## Requirements

- Go 1.25+ to build
- Administrator (Windows) or root (Linux/macOS) to run
- macOS: `diskutil` (included with the OS)

## License

MIT - see [LICENSE](LICENSE) file.
