// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

// Package ata provides ATA/SATA drive identification, APM queries, and APM set
// operations for HDD APM Tool. Platform-specific implementations live in
// ata_linux.go, ata_windows.go, and ata_darwin.go.
package ata

import "encoding/binary"

// IdentifyData represents the 512-byte ATA IDENTIFY DEVICE response.
type IdentifyData [256]uint16

// ParseIdentify parses raw 512 bytes into an IdentifyData (little-endian words).
func ParseIdentify(raw []byte) IdentifyData {
	var id IdentifyData
	for i := 0; i < 256 && i*2+1 < len(raw); i++ {
		id[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	return id
}

// Model returns the drive model string (words 27-46).
func (id IdentifyData) Model() string {
	return ataStringFromWords(id[27:47])
}

// SerialNumber returns the serial number string (words 10-19).
func (id IdentifyData) SerialNumber() string {
	return ataStringFromWords(id[10:20])
}

// FirmwareRevision returns the firmware revision string (words 23-26).
func (id IdentifyData) FirmwareRevision() string {
	return ataStringFromWords(id[23:27])
}

// IsAPMSupported reports whether the APM feature set is supported (word 83, bit 3).
// Word 83 must have bit 14=1 and bit 15=0 for the field to be valid per the ATA spec.
func (id IdentifyData) IsAPMSupported() bool {
	w83 := id[83]
	if w83&0xC000 != 0x4000 {
		return false
	}
	return w83&(1<<3) != 0
}

// IsAPMEnabled reports whether APM is currently enabled (word 86, bit 3).
func (id IdentifyData) IsAPMEnabled() bool {
	return id[86]&(1<<3) != 0
}

// CurrentAPMLevel returns the current APM value (word 91, low byte).
// Only meaningful when IsAPMEnabled() returns true.
func (id IdentifyData) CurrentAPMLevel() uint8 {
	return uint8(id[91] & 0xFF)
}

// IsSSD reports whether the drive is a solid-state device.
// ATA word 217 value 0x0001 indicates a non-rotating (SSD) media.
func (id IdentifyData) IsSSD() bool {
	return id[217] == 0x0001
}

// RotationRate returns the nominal media rotation rate from word 217.
// 0 = not reported, 1 = SSD (non-rotating), other values = RPM.
func (id IdentifyData) RotationRate() uint16 {
	return id[217]
}

// ataStringFromWords converts an ATA word slice to a trimmed ASCII string.
// ATA strings store two characters per word with the bytes swapped (high byte
// holds the first character of the pair).
func ataStringFromWords(words []uint16) string {
	buf := make([]byte, len(words)*2)
	for i, w := range words {
		buf[i*2] = byte(w >> 8)
		buf[i*2+1] = byte(w & 0xFF)
	}
	end := len(buf)
	for end > 0 && (buf[end-1] == ' ' || buf[end-1] == 0) {
		end--
	}
	start := 0
	for start < end && buf[start] == ' ' {
		start++
	}
	return string(buf[start:end])
}

// DiskInfo holds information about a discovered disk.
type DiskInfo struct {
	Path         string // Device path (e.g. /dev/sda or \\.\PhysicalDrive0)
	Model        string
	Serial       string
	Firmware     string
	IsSSDDrive   bool
	RotationRPM  uint16
	APMSupported bool
	APMEnabled   bool
	APMLevel     uint8  // Current APM level (1-254); 0 when disabled
	Interface    string // "SATA", "NVMe", "USB/SATA", etc.
}

// APMResult holds the outcome of an APM set operation on a single disk.
type APMResult struct {
	Disk       DiskInfo
	Success    bool
	Skipped    bool
	SkipReason string
	Error      error
	OldLevel   uint8
	NewLevel   uint8
	WasEnabled bool
}

// APM level constants.
const (
	APMMinPower        uint8 = 1   // Minimum power, maximum spindown aggressiveness
	APMWithoutSpindown uint8 = 128 // No spindown, intermediate power saving
	APMMaxPerformance  uint8 = 254 // Maximum performance with APM enabled
	APMDisable         uint8 = 255 // Disable APM entirely
)
