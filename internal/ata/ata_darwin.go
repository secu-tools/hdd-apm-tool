// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build darwin

package ata

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// macOS SCSI pass-through ioctl: DKIOCIOCSCSICOMMAND
// = _IOWR('d', 107, dk_ioctl_scsicommand_t)
// sizeof(dk_ioctl_scsicommand_t) = 64  =>  0xC040646B
//
// Encoding: IOC_INOUT=0xC0000000 | (size<<16) | ('d'<<8) | num
//
//	size=0x40=64, group='d'=0x64, num=107=0x6B
//	=> 0xC0000000 | 0x00400000 | 0x00006400 | 0x0000006B = 0xC040646B
const dkiociocscsicommand uintptr = 0xC040646B

// SCSI ATA Translation opcodes and ATA command constants (darwin-local).
const (
	darwinSATPassthrough16  = 0x85
	darwinAtaCmdIdentify    = 0xEC
	darwinAtaCmdSetFeatures = 0xEF
	darwinApmFeatureEnable  = 0x05
	darwinApmFeatureDisable = 0x85

	// DK_IOCS_COMMAND direction field values.
	dkDirNone    uint8 = 0 // no data transfer
	dkDirFromDev uint8 = 1 // device -> host (read)
	dkDirToDev   uint8 = 2 // host -> device (write)
)

// dkIOCSCommand is DK_IOCS_COMMAND (32 bytes, packed).
// Defined in <sys/disk.h> as DK_IOCS_COMMAND.
type dkIOCSCommand struct {
	CDB        [16]byte
	CDBSize    uint8
	Direction  uint8
	TimeoutSec uint8
	pad0       [13]byte
}

// dkSenseData is dk_sense_data_t (22 bytes, packed).
type dkSenseData struct {
	ResponseCode uint8
	SenseKey     uint8
	ASC          uint8
	ASCQ         uint8
	Data         [18]byte
}

// dkIoctlSCSICommand is dk_ioctl_scsicommand_t.
// Layout: 8+8+8+8 + 1+3 + 22 + 6_trail = 64 bytes.
// Command and Buffer are unsafe.Pointer so the GC tracks the pointed-to
// objects while the struct is live.
type dkIoctlSCSICommand struct {
	Command     unsafe.Pointer // pointer to dkIOCSCommand (GC-tracked)
	CommandSize uint64
	Buffer      unsafe.Pointer // pointer to data buffer (GC-tracked)
	BufferSize  uint64
	SCSIStatus  uint8
	pad0        [3]byte
	SenseData   dkSenseData
	pad1        [6]byte
}

// EnumerateDisks discovers whole physical disks on macOS using diskutil.
// Returns /dev/rdisk* paths (raw character device -- required for ioctl).
func EnumerateDisks() ([]string, error) {
	out, err := exec.Command("diskutil", "list", "-plist").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil list: %w", err)
	}
	diskIDs := parsePlistWholeDisks(string(out))
	var result []string
	for _, id := range diskIDs {
		// Raw disk device is required for SCSI pass-through ioctls.
		// /dev/rdiskN is the character device; /dev/diskN is the block device.
		raw := "/dev/r" + id
		if _, err := os.Stat(raw); err == nil {
			result = append(result, raw)
		}
	}
	return result, nil
}

// IdentifyDevice sends ATA IDENTIFY DEVICE via SCSI ATA Translation (SAT16)
// to the raw disk device at path (/dev/rdiskN). Root access is required.
func IdentifyDevice(path string) ([]byte, error) {
	fd, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer fd.Close()
	return satIdentify(fd)
}

// SetAPM sets the APM level on the device at path via SAT16 pass-through.
// level 1-254 enables APM; level 255 (APMDisable) disables APM entirely.
func SetAPM(path string, level uint8) error {
	fd, err := os.OpenFile(path, os.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open %s for write: %w", path, err)
	}
	defer fd.Close()

	var feature, count uint8
	if level == APMDisable {
		feature = darwinApmFeatureDisable
		count = 0
	} else {
		feature = darwinApmFeatureEnable
		count = level
	}
	return satSetFeatures(fd, feature, count)
}

// DetectInterface returns the bus type for a macOS disk path.
// Runs diskutil info -plist to query the kernel's view of the connection.
// Expects path in the form /dev/rdiskN or /dev/diskN.
func DetectInterface(path string) string {
	// Convert /dev/rdiskN -> diskN or /dev/diskN -> diskN for diskutil.
	diskID := strings.TrimPrefix(path, "/dev/r")
	if diskID == path {
		diskID = strings.TrimPrefix(path, "/dev/")
	}
	out, err := exec.Command("diskutil", "info", "-plist", "/dev/"+diskID).Output()
	if err != nil {
		return "Unknown"
	}
	proto := parsePlistString(string(out), "BusProtocol")
	switch proto {
	case "NVMe":
		return "NVMe"
	case "SATA", "AHCI":
		return "SATA"
	case "USB":
		return "USB/SATA"
	case "Thunderbolt":
		return "Thunderbolt"
	case "FireWire":
		return "FireWire"
	case "PCIe":
		return "PCIe"
	default:
		if proto != "" {
			return proto
		}
		return "Unknown"
	}
}

// IsNVMeDevice reports whether the device at path is an NVMe drive.
func IsNVMeDevice(path string) bool {
	return DetectInterface(path) == "NVMe"
}

// satIdentify sends ATA IDENTIFY DEVICE via SAT16 pass-through.
func satIdentify(fd *os.File) ([]byte, error) {
	data := make([]byte, 512)
	cmd := &dkIOCSCommand{
		CDBSize:    16,
		Direction:  dkDirFromDev,
		TimeoutSec: 5,
	}
	// ATA PASS-THROUGH(16) CDB for IDENTIFY DEVICE (PIO Data-In).
	cmd.CDB[0] = darwinSATPassthrough16
	cmd.CDB[1] = (4 << 1) // protocol = PIO Data-In
	cmd.CDB[2] = 0x0E     // T_DIR=device->host, BYTE_BLOCK=1, T_LENGTH=sector-count
	cmd.CDB[6] = 1        // sector count [7:0] = 1
	cmd.CDB[13] = 0xA0    // device register (master)
	cmd.CDB[14] = darwinAtaCmdIdentify

	if err := dkioSCSICommand(fd, cmd, data); err != nil {
		return nil, fmt.Errorf("SAT16 IDENTIFY on %s: %w", fd.Name(), err)
	}
	for _, b := range data {
		if b != 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("SAT16 IDENTIFY: empty response from %s", fd.Name())
}

// satSetFeatures sends ATA SET FEATURES via SAT16 pass-through (non-data command).
func satSetFeatures(fd *os.File, feature, count uint8) error {
	cmd := &dkIOCSCommand{
		CDBSize:    16,
		Direction:  dkDirNone,
		TimeoutSec: 5,
	}
	// ATA PASS-THROUGH(16) CDB for SET FEATURES (Non-data protocol).
	cmd.CDB[0] = darwinSATPassthrough16
	cmd.CDB[1] = (3 << 1) // protocol = Non-data
	cmd.CDB[2] = 0x00     // no data transfer
	cmd.CDB[4] = feature  // features [7:0]
	cmd.CDB[6] = count    // count [7:0] = APM level
	cmd.CDB[13] = 0xA0    // device register
	cmd.CDB[14] = darwinAtaCmdSetFeatures

	return dkioSCSICommand(fd, cmd, nil)
}

// dkioSCSICommand issues DKIOCIOCSCSICOMMAND to dispatch a SCSI command.
// buf may be nil for non-data commands.
func dkioSCSICommand(fd *os.File, cmd *dkIOCSCommand, buf []byte) error {
	io := dkIoctlSCSICommand{
		Command:     unsafe.Pointer(cmd),
		CommandSize: uint64(unsafe.Sizeof(*cmd)),
	}
	if len(buf) > 0 {
		io.Buffer = unsafe.Pointer(&buf[0])
		io.BufferSize = uint64(len(buf))
	}

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		fd.Fd(),
		dkiociocscsicommand,
		uintptr(unsafe.Pointer(&io)),
	)
	// cmd and buf must stay reachable until the kernel is done with them.
	runtime.KeepAlive(cmd)
	runtime.KeepAlive(buf)

	if errno != 0 {
		return fmt.Errorf("DKIOCIOCSCSICOMMAND: %v", errno)
	}
	if io.SCSIStatus != 0 {
		return fmt.Errorf("DKIOCIOCSCSICOMMAND: SCSI status=0x%02X key=0x%02X ASC=0x%02X ASCQ=0x%02X",
			io.SCSIStatus, io.SenseData.SenseKey, io.SenseData.ASC, io.SenseData.ASCQ)
	}
	return nil
}

// parsePlistWholeDisks extracts the "WholeDisks" string array from diskutil's
// plist XML output (e.g. output of "diskutil list -plist").
// Returns disk identifiers like ["disk0", "disk1"].
func parsePlistWholeDisks(plist string) []string {
	const key = "<key>WholeDisks</key>"
	idx := strings.Index(plist, key)
	if idx < 0 {
		return nil
	}
	rest := plist[idx+len(key):]
	aStart := strings.Index(rest, "<array>")
	if aStart < 0 {
		return nil
	}
	aEnd := strings.Index(rest, "</array>")
	if aEnd < 0 || aEnd <= aStart {
		return nil
	}
	return extractPlistStrings(rest[aStart+7 : aEnd])
}

// parsePlistString extracts the first value for the given key from a plist.
// Returns "true"/"false" for boolean keys, the string content for <string> keys,
// and "" when the key is not found.
func parsePlistString(plist, key string) string {
	marker := "<key>" + key + "</key>"
	idx := strings.Index(plist, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(plist[idx+len(marker):])
	if strings.HasPrefix(rest, "<true/>") {
		return "true"
	}
	if strings.HasPrefix(rest, "<false/>") {
		return "false"
	}
	sStart := strings.Index(rest, "<string>")
	if sStart < 0 {
		return ""
	}
	sEnd := strings.Index(rest[sStart+8:], "</string>")
	if sEnd < 0 {
		return ""
	}
	return rest[sStart+8 : sStart+8+sEnd]
}

// extractPlistStrings extracts all <string>value</string> entries from a plist section.
func extractPlistStrings(section string) []string {
	var out []string
	rest := section
	for {
		sStart := strings.Index(rest, "<string>")
		if sStart < 0 {
			break
		}
		sEnd := strings.Index(rest[sStart+8:], "</string>")
		if sEnd < 0 {
			break
		}
		out = append(out, rest[sStart+8:sStart+8+sEnd])
		rest = rest[sStart+8+sEnd+9:]
	}
	return out
}
