# HDD APM Tool

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![CI](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/ci.yml/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/ci.yml)
[![Build](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/build.yml/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/build.yml)
[![CodeQL](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/github-code-scanning/codeql)
[![Dependency Graph](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/dependabot/update-graph/badge.svg)](https://github.com/secu-tools/hdd-apm-tool/actions/workflows/dependabot/update-graph)

## Introduction
ATA Advanced Power Management (APM) configuration tool for spinning HDD drives
on Windows, Linux, and macOS. No external dependencies - issues raw ATA commands
directly via OS APIs.

## How It Works

1. Enumerates all disk devices (Windows: `\\.\PhysicalDriveN`, Linux: `/dev/sd*`, macOS: `/dev/rdiskN`)
2. Identifies each drive using ATA IDENTIFY DEVICE command (0xEC)
3. Checks APM capability via word 83 bit 3 of the IDENTIFY response
4. Skips:
   - NVMe drives (use NVMe APST, not ATA APM)
   - SSD drives (APM spindown is irrelevant for solid-state devices)
   - Any drive where IDENTIFY reports APM not supported
5. Sets APM using ATA SET FEATURES (0xEF) subcommand 0x05 (enable) or 0x85 (disable)
6. Logs results to stdout; writes to a log file only when `--logdir` is specified

The tool is a one-shot application: it applies APM settings to every eligible
drive and exits. When installed as a service, the OS runs it once at each boot
to restore settings that are lost after a power cycle.

### Passthrough Methods (handles USB drives)

| Method | Platform | Use Case |
|--------|----------|----------|
| IOCTL_ATA_PASS_THROUGH | Windows | Direct SATA/AHCI drives |
| IOCTL_SCSI_PASS_THROUGH + SAT12 CDB | Windows | SATA/USB bridges (buffered) |
| IOCTL_SCSI_PASS_THROUGH_DIRECT + SAT16 CDB | Windows | USB Mass Storage bridges |
| HDIO_DRIVE_CMD ioctl | Linux | Direct SATA drives |
| SG_IO ioctl + SAT16 CDB | Linux | USB/SATA bridges |
| DKIOCIOCSCSICOMMAND ioctl + SAT16 CDB | macOS | SATA/USB drives via IOKit |

## APM Levels

| Level | Meaning |
|-------|---------|
| 1 - 127 | APM enabled, spindown allowed (aggressive power saving) |
| 128 - 254 | APM enabled, no spindown (moderate power saving) |
| 254 | Maximum performance with APM enabled |
| 255 | Disable APM entirely (default) |

## Quick Start

```bash
# Build
go build -o hdd-apm-tool ./

# Show version
./hdd-apm-tool --version

# Dry run - show what would be done
sudo ./hdd-apm-tool --apm 128 --dry-run

# Apply APM level 128 to all HDD drives
sudo ./hdd-apm-tool --apm 128

# Disable APM
sudo ./hdd-apm-tool --apm 255
```

On Windows, administrator privileges are required. If the tool is not already
running as Administrator, it will prompt for UAC elevation automatically.

On Linux, run with sudo.

## Service Installation

APM settings are volatile: drives reset to factory defaults after a power
cycle. Install HDD APM Tool as a system service to reapply settings automatically
on every boot.

The service is run-once by design: at each boot the OS starts the tool, it
applies APM to all eligible drives, then exits with code 0. The OS does not
keep the process running between boots.

- **Linux (systemd)**: unit type is `oneshot` with `RemainAfterExit=yes`. After
  the tool exits, `systemctl status` shows `active (exited)`, not `failed`.
  The unit is not set to restart on failure; if APM cannot be applied, check
  `journalctl -u hdd-apm-tool` for the error.
- **Windows (SCM)**: service type is `auto-start`. After the tool exits with 0,
  the SCM shows the service as `Stopped`. On next boot the SCM starts it again.
  Three automatic retries (with increasing delays) are configured for the case
  where disk drivers are not yet ready when the service first starts.
  When started manually from Services.msc, the service dwells briefly in the
  Running state after completing its work so that Windows does not show the
  "started and then stopped" warning.
- **macOS (launchd)**: a plist with `RunAtLoad=true` and `KeepAlive=false` is
  installed to `/Library/LaunchDaemons/`. launchd runs the tool once at boot
  and does not restart it. After the tool exits, check logs at
  `/var/log/hdd-apm-tool/`.

```bash
# Linux (requires root)
sudo ./hdd-apm-tool --install --apm 128

# macOS (requires root)
sudo ./hdd-apm-tool --install --apm 128

# Windows (run as Administrator, or the UAC prompt will appear)
hdd-apm-tool.exe --install --apm 128

# Uninstall
sudo ./hdd-apm-tool --uninstall
hdd-apm-tool.exe --uninstall
```

The installer prompts for an optional label so that multiple instances with
different APM levels can coexist on the same machine.

On Linux, a systemd unit is created under `/etc/systemd/system/`.
On macOS, a launchd plist is created under `/Library/LaunchDaemons/`.
On Windows, a Windows Service is registered via `sc.exe`.

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

By default, output goes to stdout only. Specify `--logdir` to also write a
rotating log file to that directory:

```bash
sudo ./hdd-apm-tool --apm 128 --logdir /var/log/hdd-apm-tool
```

When installed as a service and `--logdir` was supplied to `--install`, the
log directory is preserved in the service arguments and used on every start.

## Building

```powershell
# Windows - CMD
build.cmd
build.cmd -all
build.cmd -test

# Windows - PowerShell
.\build.ps1                   # windows/amd64 + linux/amd64
.\build.ps1 -linux            # linux/amd64 + linux/arm64
.\build.ps1 -darwin           # darwin/amd64 + darwin/arm64
.\build.ps1 -all              # all platform/arch combinations
.\build.ps1 -test             # run tests
.\build.ps1 -coverage         # run tests with coverage report
.\build.ps1 -clean            # remove build artifacts
```

```bash
# Linux/macOS
./build.sh                    # linux/amd64 + windows/amd64
./build.sh -linux             # linux/amd64 + linux/arm64
./build.sh -darwin            # darwin/amd64 + darwin/arm64
./build.sh -all               # all platform/arch combinations
./build.sh -test              # run tests
./build.sh -coverage          # run tests with coverage report
./build.sh -clean             # remove build artifacts
```

## Platform Notes

### Linux

Uses HDIO_GET_IDENTITY and HDIO_DRIVE_CMD ioctls for direct SATA drives, with
SG_IO SAT16 PASS-THROUGH fallback for USB-attached drives. Requires root.

Device files are opened with `O_NONBLOCK` to avoid blocking on drives that are
not yet ready. CK_COND=0 is used in the SG_IO CDB for maximum compatibility
with USB-to-SATA bridge chipsets.

### Windows

Uses a three-stage passthrough chain for maximum compatibility:
1. `IOCTL_ATA_PASS_THROUGH` - direct SATA/AHCI drives
2. `IOCTL_SCSI_PASS_THROUGH` + SAT12 CDB - most SATA/USB bridges
3. `IOCTL_SCSI_PASS_THROUGH_DIRECT` + SAT16 CDB - USB Mass Storage bridges

The third method (SPTD) is required for USB drives because the USB Mass Storage
class driver (`usbstor.sys`) does not support the buffered SCSI pass-through
IOCTL. SAT16 (16-byte CDB) is used in the SPTD path for wider USB bridge
chipset support. CK_COND=0 is used in all CDBs for bridge compatibility.

UAC elevation is requested automatically when the tool is not already running
as Administrator.

### macOS

Uses `diskutil list -plist` to enumerate whole physical disks and `diskutil info
-plist` to query the bus protocol (NVMe, SATA, USB, Thunderbolt). ATA commands
are issued via the `DKIOCIOCSCSICOMMAND` ioctl using raw disk device nodes
(`/dev/rdiskN`). SAT16 ATA PASS-THROUGH CDBs are used for IDENTIFY DEVICE and
SET FEATURES. Requires root.

**Notes for macOS users:**
- Most modern Macs have NVMe or SATA SSD internal storage. APM is not applicable
  to NVMe drives or SSDs; the tool will enumerate and skip them automatically.
- External spinning HDDs connected via USB or Thunderbolt are fully supported.
  The tool reads drive identity and applies APM the same as on Linux/Windows.
- Pure Go build (CGO_ENABLED=0): no Xcode or native SDK is required to run or
  cross-compile. darwin/amd64 and darwin/arm64 binaries are cross-compiled from
  the Linux CI runner.

Example usage with an external USB hard drive:

```bash
# List drives - shows interfaces and APM state
sudo ./hdd-apm-tool --dry-run

# Apply APM level 128 (no spindown, moderate power saving)
sudo ./hdd-apm-tool --apm 128

# Install as a launchd daemon (runs at boot)
sudo ./hdd-apm-tool --install --apm 128

# Check service status
launchctl list | grep com.secu-tools.hdd-apm-tool

# View logs
tail -f /var/log/hdd-apm-tool/hdd-apm-tool.stdout.log

# Uninstall
sudo ./hdd-apm-tool --uninstall
```

## Running Tests

```powershell
# Windows
.\build.ps1 -test
.\build.ps1 -coverage
```

```bash
# Linux/macOS
./build.sh -test
./build.sh -coverage
```

## Requirements

- Go 1.25+ to build
- Administrator (Windows) or root (Linux/macOS) to run (prompted automatically on Windows)
- No external tools required (no hdparm, no smartctl, no Xcode)
- macOS: `diskutil` must be available (included in all macOS installations)

## License

MIT - see [LICENSE](LICENSE) file.
