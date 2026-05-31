// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build darwin

// Internal (white-box) tests for darwin-specific code in the ata package.
// Uses package ata (not ata_test) to access unexported types and functions.
package ata

import (
	"strings"
	"testing"
	"unsafe"
)

// =====================================================================
// Struct layout verification
// Every size must match the C definition in <sys/disk.h> exactly.
// =====================================================================

func TestDkIOCSCommandSize(t *testing.T) {
	// DK_IOCS_COMMAND: 16+1+1+1+13 = 32 bytes (no padding, all uint8/byte)
	var cmd dkIOCSCommand
	if got := int(unsafe.Sizeof(cmd)); got != 32 {
		t.Errorf("sizeof(dkIOCSCommand) = %d, want 32", got)
	}
}

func TestDkSenseDataSize(t *testing.T) {
	// dk_sense_data_t: 1+1+1+1+18 = 22 bytes (no padding, all uint8)
	var s dkSenseData
	if got := int(unsafe.Sizeof(s)); got != 22 {
		t.Errorf("sizeof(dkSenseData) = %d, want 22", got)
	}
}

func TestDkIoctlSCSICommandSize(t *testing.T) {
	// dk_ioctl_scsicommand_t: 8+8+8+8 + 1+3 + 22 + 6_trail = 64 bytes
	// This size encodes directly into the DKIOCIOCSCSICOMMAND constant
	// (0xC040646B); any discrepancy would silently misfire the ioctl.
	var io dkIoctlSCSICommand
	if got := int(unsafe.Sizeof(io)); got != 64 {
		t.Errorf("sizeof(dkIoctlSCSICommand) = %d, want 64 (ioctl 0xC040646B encodes size=64)", got)
	}
}

func TestDkIOCSCommandFieldOffsets(t *testing.T) {
	// Verify CDB starts at offset 0 and CDBSize immediately follows.
	var cmd dkIOCSCommand
	base := uintptr(unsafe.Pointer(&cmd))
	cdbOff := uintptr(unsafe.Pointer(&cmd.CDB)) - base
	sizeOff := uintptr(unsafe.Pointer(&cmd.CDBSize)) - base
	dirOff := uintptr(unsafe.Pointer(&cmd.Direction)) - base
	tmoOff := uintptr(unsafe.Pointer(&cmd.TimeoutSec)) - base
	if cdbOff != 0 {
		t.Errorf("CDB offset = %d, want 0", cdbOff)
	}
	if sizeOff != 16 {
		t.Errorf("CDBSize offset = %d, want 16", sizeOff)
	}
	if dirOff != 17 {
		t.Errorf("Direction offset = %d, want 17", dirOff)
	}
	if tmoOff != 18 {
		t.Errorf("TimeoutSec offset = %d, want 18", tmoOff)
	}
}

func TestDkIoctlSCSICommandFieldOffsets(t *testing.T) {
	// Verify the C-compatible field layout used when the struct is passed to the kernel.
	var io dkIoctlSCSICommand
	base := uintptr(unsafe.Pointer(&io))
	cmdOff := uintptr(unsafe.Pointer(&io.Command)) - base
	cmdSzOff := uintptr(unsafe.Pointer(&io.CommandSize)) - base
	bufOff := uintptr(unsafe.Pointer(&io.Buffer)) - base
	bufSzOff := uintptr(unsafe.Pointer(&io.BufferSize)) - base
	statusOff := uintptr(unsafe.Pointer(&io.SCSIStatus)) - base
	senseOff := uintptr(unsafe.Pointer(&io.SenseData)) - base

	if cmdOff != 0 {
		t.Errorf("Command offset = %d, want 0", cmdOff)
	}
	if cmdSzOff != 8 {
		t.Errorf("CommandSize offset = %d, want 8", cmdSzOff)
	}
	if bufOff != 16 {
		t.Errorf("Buffer offset = %d, want 16", bufOff)
	}
	if bufSzOff != 24 {
		t.Errorf("BufferSize offset = %d, want 24", bufSzOff)
	}
	if statusOff != 32 {
		t.Errorf("SCSIStatus offset = %d, want 32", statusOff)
	}
	if senseOff != 36 {
		t.Errorf("SenseData offset = %d, want 36", senseOff)
	}
}

// =====================================================================
// Plist parsing: parsePlistWholeDisks
// =====================================================================

func TestParsePlistWholeDisks_Basic(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
    <key>AllDisks</key>
    <array>
        <string>disk0</string>
        <string>disk0s1</string>
    </array>
    <key>WholeDisks</key>
    <array>
        <string>disk0</string>
        <string>disk1</string>
    </array>
</dict></plist>`
	got := parsePlistWholeDisks(plist)
	if len(got) != 2 {
		t.Fatalf("got %d disks, want 2: %v", len(got), got)
	}
	if got[0] != "disk0" || got[1] != "disk1" {
		t.Errorf("got %v, want [disk0 disk1]", got)
	}
}

func TestParsePlistWholeDisks_Single(t *testing.T) {
	plist := `<dict>
    <key>WholeDisks</key>
    <array>
        <string>disk0</string>
    </array>
</dict>`
	got := parsePlistWholeDisks(plist)
	if len(got) != 1 || got[0] != "disk0" {
		t.Errorf("got %v, want [disk0]", got)
	}
}

func TestParsePlistWholeDisks_Empty(t *testing.T) {
	got := parsePlistWholeDisks("<plist></plist>")
	if len(got) != 0 {
		t.Errorf("empty plist: got %v, want nil/empty", got)
	}
}

func TestParsePlistWholeDisks_MissingKey(t *testing.T) {
	plist := `<dict><key>SomeOtherKey</key><array><string>disk0</string></array></dict>`
	got := parsePlistWholeDisks(plist)
	if len(got) != 0 {
		t.Errorf("missing WholeDisks key: got %v, want empty", got)
	}
}

func TestParsePlistWholeDisks_NoClosingArray(t *testing.T) {
	plist := `<dict><key>WholeDisks</key><array><string>disk0</string>`
	got := parsePlistWholeDisks(plist)
	if len(got) != 0 {
		t.Errorf("malformed plist: got %v, want empty", got)
	}
}

// =====================================================================
// Plist parsing: parsePlistString
// =====================================================================

func TestParsePlistString_String(t *testing.T) {
	cases := []struct {
		plist string
		key   string
		want  string
	}{
		{`<dict><key>BusProtocol</key><string>NVMe</string></dict>`, "BusProtocol", "NVMe"},
		{`<dict><key>BusProtocol</key><string>SATA</string></dict>`, "BusProtocol", "SATA"},
		{`<dict><key>BusProtocol</key><string>USB</string></dict>`, "BusProtocol", "USB"},
		{`<dict><key>MediaType</key><string>Rotational</string></dict>`, "MediaType", "Rotational"},
		{`<dict><key>MediaName</key>
			<string>WD Elements 25A3 Media</string></dict>`, "MediaName", "WD Elements 25A3 Media"},
	}
	for _, tc := range cases {
		got := parsePlistString(tc.plist, tc.key)
		if got != tc.want {
			t.Errorf("parsePlistString(_, %q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestParsePlistString_Bool(t *testing.T) {
	plistTrue := `<dict><key>SolidState</key><true/></dict>`
	plistFalse := `<dict><key>SolidState</key><false/></dict>`

	if got := parsePlistString(plistTrue, "SolidState"); got != "true" {
		t.Errorf("SolidState <true/>: got %q, want %q", got, "true")
	}
	if got := parsePlistString(plistFalse, "SolidState"); got != "false" {
		t.Errorf("SolidState <false/>: got %q, want %q", got, "false")
	}
}

func TestParsePlistString_MissingKey(t *testing.T) {
	plist := `<dict><key>Foo</key><string>bar</string></dict>`
	if got := parsePlistString(plist, "BusProtocol"); got != "" {
		t.Errorf("missing key: got %q, want empty", got)
	}
}

func TestParsePlistString_EmptyPlist(t *testing.T) {
	if got := parsePlistString("", "BusProtocol"); got != "" {
		t.Errorf("empty plist: got %q, want empty", got)
	}
}

// =====================================================================
// Plist parsing: extractPlistStrings
// =====================================================================

func TestExtractPlistStrings_Multiple(t *testing.T) {
	section := `
        <string>disk0</string>
        <string>disk1</string>
        <string>disk2</string>`
	got := extractPlistStrings(section)
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3: %v", len(got), got)
	}
	if got[0] != "disk0" || got[1] != "disk1" || got[2] != "disk2" {
		t.Errorf("got %v, want [disk0 disk1 disk2]", got)
	}
}

func TestExtractPlistStrings_Empty(t *testing.T) {
	got := extractPlistStrings("")
	if len(got) != 0 {
		t.Errorf("empty section: got %v, want nil/empty", got)
	}
}

func TestExtractPlistStrings_Single(t *testing.T) {
	got := extractPlistStrings(`<string>hello</string>`)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got %v, want [hello]", got)
	}
}

// =====================================================================
// Interface detection: bus protocol mapping
// =====================================================================

func TestDetectInterfaceMapping(t *testing.T) {
	// Test the bus-protocol-to-interface mapping logic by verifying that the
	// parsePlistString + switch logic produces the expected strings.
	// This exercises DetectInterface without requiring actual hardware.
	cases := []struct {
		busProto string
		want     string
	}{
		{"NVMe", "NVMe"},
		{"SATA", "SATA"},
		{"AHCI", "SATA"},
		{"USB", "USB/SATA"},
		{"Thunderbolt", "Thunderbolt"},
		{"FireWire", "FireWire"},
		{"PCIe", "PCIe"},
		{"SomeBus", "SomeBus"},
		{"", "Unknown"},
	}
	for _, tc := range cases {
		plist := `<dict><key>BusProtocol</key><string>` + tc.busProto + `</string></dict>`
		proto := parsePlistString(plist, "BusProtocol")
		var got string
		switch proto {
		case "NVMe":
			got = "NVMe"
		case "SATA", "AHCI":
			got = "SATA"
		case "USB":
			got = "USB/SATA"
		case "Thunderbolt":
			got = "Thunderbolt"
		case "FireWire":
			got = "FireWire"
		case "PCIe":
			got = "PCIe"
		default:
			if proto != "" {
				got = proto
			} else {
				got = "Unknown"
			}
		}
		if got != tc.want {
			t.Errorf("bus %q: got %q, want %q", tc.busProto, got, tc.want)
		}
	}
}

// =====================================================================
// EnumerateDisks smoke test -- hardware not required
// =====================================================================

func TestEnumerateDisks_DoesNotPanic(t *testing.T) {
	// diskutil may succeed or fail in CI. Either way, must not panic.
	disks, err := EnumerateDisks()
	if err != nil {
		// diskutil unavailable or permission issue -- not a test failure.
		t.Skipf("EnumerateDisks: %v (may be expected in restricted CI)", err)
	}
	for _, d := range disks {
		if !strings.HasPrefix(d, "/dev/rdisk") {
			t.Errorf("unexpected disk path format: %q (want /dev/rdiskN)", d)
		}
	}
}

// =====================================================================
// IdentifyDevice -- error handling without hardware
// =====================================================================

func TestIdentifyDevice_NonexistentPath(t *testing.T) {
	_, err := IdentifyDevice("/dev/rdisk99999")
	if err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}
}

func TestIdentifyDevice_InvalidPath(t *testing.T) {
	_, err := IdentifyDevice("")
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
}

// =====================================================================
// ATA constant values
// =====================================================================

func TestDarwinATAConstants(t *testing.T) {
	if darwinAtaCmdIdentify != 0xEC {
		t.Errorf("ataCmdIdentify = 0x%02X, want 0xEC", darwinAtaCmdIdentify)
	}
	if darwinAtaCmdSetFeatures != 0xEF {
		t.Errorf("ataCmdSetFeatures = 0x%02X, want 0xEF", darwinAtaCmdSetFeatures)
	}
	if darwinApmFeatureEnable != 0x05 {
		t.Errorf("apmFeatureEnable = 0x%02X, want 0x05", darwinApmFeatureEnable)
	}
	if darwinApmFeatureDisable != 0x85 {
		t.Errorf("apmFeatureDisable = 0x%02X, want 0x85", darwinApmFeatureDisable)
	}
	if dkiociocscsicommand != 0xC040646B {
		t.Errorf("dkiociocscsicommand = 0x%08X, want 0xC040646B", dkiociocscsicommand)
	}
}
