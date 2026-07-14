// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

package ata_test

import (
	"encoding/binary"
	"testing"

	"github.com/secu-tools/hdd-apm-tool/internal/ata"
)

// buildIdentifyData constructs a synthetic 512-byte ATA IDENTIFY DEVICE
// response for use in unit tests.
func buildIdentifyData(opts identifyOpts) []byte {
	raw := make([]byte, 512)
	words := make([]uint16, 256)

	if opts.model != "" {
		setATAString(words[27:47], opts.model)
	}
	if opts.serial != "" {
		setATAString(words[10:20], opts.serial)
	}
	if opts.firmware != "" {
		setATAString(words[23:27], opts.firmware)
	}

	words[82] = opts.word82
	// Ensure validity bits (bit14=1, bit15=0) are set unless the test explicitly
	// provides an invalid value for word83.
	words[83] = opts.word83 | 0x4000
	words[85] = opts.word85
	words[86] = opts.word86
	words[91] = opts.word91
	words[217] = opts.word217

	for i, w := range words {
		binary.LittleEndian.PutUint16(raw[i*2:i*2+2], w)
	}
	return raw
}

type identifyOpts struct {
	model    string
	serial   string
	firmware string
	word82   uint16
	word83   uint16
	word85   uint16
	word86   uint16
	word91   uint16
	word217  uint16
}

// setATAString encodes a plain string into an ATA word slice (bytes swapped
// within each word, space-padded to full width).
func setATAString(words []uint16, s string) {
	totalBytes := len(words) * 2
	padded := s
	for len(padded) < totalBytes {
		padded += " "
	}
	for i := range words {
		hi := padded[i*2]
		lo := byte(' ')
		if i*2+1 < len(padded) {
			lo = padded[i*2+1]
		}
		words[i] = uint16(hi)<<8 | uint16(lo)
	}
}

// =====================================================================
// ParseIdentify field extraction
// =====================================================================

func TestParseIdentify_Model(t *testing.T) {
	cases := []struct{ name, model, want string }{
		{"WD Purple", "WDC WD121PURZ-85GUCY0", "WDC WD121PURZ-85GUCY0"},
		{"Samsung SSD", "Samsung SSD 840 EVO 1TB", "Samsung SSD 840 EVO 1TB"},
		{"ADATA SU650", "ADATA SU650", "ADATA SU650"},
		{"Seagate", "ST4000DM005-2DP166", "ST4000DM005-2DP166"},
		{"HGST", "HGST HTS721010A9E630", "HGST HTS721010A9E630"},
		{"Toshiba", "TOSHIBA MQ01ABD100", "TOSHIBA MQ01ABD100"},
		{"Empty model", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildIdentifyData(identifyOpts{model: tc.model})
			id := ata.ParseIdentify(raw)
			if got := id.Model(); got != tc.want {
				t.Errorf("Model() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseIdentify_SerialAndFirmware(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		serial:   "WD-WCC6Y5LS5TLP",
		firmware: "80.00A80",
	})
	id := ata.ParseIdentify(raw)
	if got := id.SerialNumber(); got != "WD-WCC6Y5LS5TLP" {
		t.Errorf("SerialNumber() = %q", got)
	}
	if got := id.FirmwareRevision(); got != "80.00A80" {
		t.Errorf("FirmwareRevision() = %q", got)
	}
}

func TestParseIdentify_EmptyBuffer(t *testing.T) {
	id := ata.ParseIdentify(make([]byte, 512))
	if id.Model() != "" {
		t.Errorf("empty buffer should give empty model, got %q", id.Model())
	}
	if id.IsAPMSupported() {
		t.Error("empty buffer should not report APM supported")
	}
	if id.IsAPMEnabled() {
		t.Error("empty buffer should not report APM enabled")
	}
}

func TestParseIdentify_ShortBuffer(t *testing.T) {
	// Should not panic even with a very short buffer.
	id := ata.ParseIdentify(make([]byte, 8))
	_ = id.Model()
}

// =====================================================================
// APM support / enabled
// =====================================================================

func TestParseIdentify_APMSupported(t *testing.T) {
	cases := []struct {
		name   string
		word83 uint16
		want   bool
	}{
		{"APM supported (WD Purple)", 0x7D69, true},
		{"APM supported (ADATA)", 0x7509, true},
		{"APM not supported (Samsung 840)", 0x7D01, false},
		{"Zero word83", 0x0000, false},
		{"Invalid validity bit15=1", 0xC008, false},
		{"Valid bits only, no APM", 0x4000, false},
		{"Valid bits + APM bit", 0x4008, true},
		{"Seagate typical", 0x7D6B, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildIdentifyData(identifyOpts{word83: tc.word83})
			id := ata.ParseIdentify(raw)
			if got := id.IsAPMSupported(); got != tc.want {
				t.Errorf("IsAPMSupported() word83=0x%04X = %v, want %v", tc.word83, got, tc.want)
			}
		})
	}
}

func TestParseIdentify_APMEnabled(t *testing.T) {
	cases := []struct {
		name   string
		word86 uint16
		want   bool
	}{
		{"APM enabled (WD Purple active)", 0xBC49, true},
		{"APM disabled (WD Purple off)", 0xBC41, false},
		{"APM enabled (ADATA)", 0xB409, true},
		{"APM disabled (Seagate)", 0xBC01, false},
		{"Zero", 0x0000, false},
		{"Only bit 3", 0x0008, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildIdentifyData(identifyOpts{word83: 0x4008, word86: tc.word86})
			id := ata.ParseIdentify(raw)
			if got := id.IsAPMEnabled(); got != tc.want {
				t.Errorf("IsAPMEnabled() word86=0x%04X = %v, want %v", tc.word86, got, tc.want)
			}
		})
	}
}

func TestParseIdentify_CurrentAPMLevel(t *testing.T) {
	cases := []struct {
		name   string
		word91 uint16
		want   uint8
	}{
		{"Level 254 (max performance)", 0x00FE, 254},
		{"Level 128 (no spindown)", 0x0080, 128},
		{"Level 1 (min power)", 0x0001, 1},
		{"Level 0 (disabled)", 0x0000, 0},
		{"High byte ignored", 0xFF80, 128},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildIdentifyData(identifyOpts{word83: 0x4008, word86: 0x0008, word91: tc.word91})
			id := ata.ParseIdentify(raw)
			if got := id.CurrentAPMLevel(); got != tc.want {
				t.Errorf("CurrentAPMLevel() word91=0x%04X = %d, want %d", tc.word91, got, tc.want)
			}
		})
	}
}

// =====================================================================
// SSD / rotation-rate detection
// =====================================================================

func TestParseIdentify_IsSSD(t *testing.T) {
	cases := []struct {
		name    string
		word217 uint16
		want    bool
	}{
		{"SSD (rotation rate = 1)", 0x0001, true},
		{"HDD 7200 RPM", 7200, false},
		{"HDD 5400 RPM", 5400, false},
		{"HDD 5900 RPM", 5900, false},
		{"Not reported", 0x0000, false},
		{"Unknown 0xFFFF", 0xFFFF, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildIdentifyData(identifyOpts{word217: tc.word217})
			id := ata.ParseIdentify(raw)
			if got := id.IsSSD(); got != tc.want {
				t.Errorf("IsSSD() word217=%d = %v, want %v", tc.word217, got, tc.want)
			}
		})
	}
}

func TestParseIdentify_RotationRate(t *testing.T) {
	cases := []struct {
		word217 uint16
		want    uint16
	}{
		{0x0001, 1},
		{7200, 7200},
		{5400, 5400},
		{0, 0},
	}
	for _, tc := range cases {
		raw := buildIdentifyData(identifyOpts{word217: tc.word217})
		id := ata.ParseIdentify(raw)
		if got := id.RotationRate(); got != tc.want {
			t.Errorf("RotationRate() word217=%d = %d, want %d", tc.word217, got, tc.want)
		}
	}
}

// =====================================================================
// APM level constants
// =====================================================================

func TestAPMLevelConstants(t *testing.T) {
	if ata.APMMinPower != 1 {
		t.Errorf("APMMinPower = %d, want 1", ata.APMMinPower)
	}
	if ata.APMWithoutSpindown != 128 {
		t.Errorf("APMWithoutSpindown = %d, want 128", ata.APMWithoutSpindown)
	}
	if ata.APMMaxPerformance != 254 {
		t.Errorf("APMMaxPerformance = %d, want 254", ata.APMMaxPerformance)
	}
	if ata.APMDisable != 255 {
		t.Errorf("APMDisable = %d, want 255", ata.APMDisable)
	}
}

// =====================================================================
// Real-drive data from CrystalDiskInfo captures
// =====================================================================

func TestRealDrive_WDPurple_APMEnabled(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "WDC WD121PURZ-85GUCY0",
		word83:  0x7D69,
		word86:  0xBC49,
		word91:  0x00FE,
		word217: 7200,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("WD Purple should support APM")
	}
	if !id.IsAPMEnabled() {
		t.Error("WD Purple should have APM enabled")
	}
	if id.CurrentAPMLevel() != 254 {
		t.Errorf("WD Purple APM level = %d, want 254", id.CurrentAPMLevel())
	}
	if id.IsSSD() {
		t.Error("WD Purple should not be SSD")
	}
	if id.RotationRate() != 7200 {
		t.Errorf("WD Purple rotation = %d, want 7200", id.RotationRate())
	}
	if id.Model() != "WDC WD121PURZ-85GUCY0" {
		t.Errorf("Model = %q", id.Model())
	}
}

func TestRealDrive_WDPurple_APMDisabled(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "WDC WD121PURZ-85GUCY0",
		word83:  0x7D69,
		word86:  0xBC41, // bit3=0: APM not enabled
		word217: 7200,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("WD Purple should support APM")
	}
	if id.IsAPMEnabled() {
		t.Error("WD Purple should have APM disabled")
	}
}

func TestRealDrive_Samsung840EVO(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "Samsung SSD 840 EVO 1TB",
		word83:  0x7D01, // bit3=0: APM NOT supported
		word86:  0xBC01,
		word217: 0x0001,
	})
	id := ata.ParseIdentify(raw)

	if id.IsAPMSupported() {
		t.Error("Samsung 840 EVO should NOT support APM")
	}
	if !id.IsSSD() {
		t.Error("Samsung 840 EVO should be SSD")
	}
}

func TestRealDrive_Samsung850PRO(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "Samsung SSD 850 PRO 1TB",
		word83:  0x7D01,
		word86:  0xBC01,
		word217: 0x0001,
	})
	id := ata.ParseIdentify(raw)

	if id.IsAPMSupported() {
		t.Error("Samsung 850 PRO should NOT support APM")
	}
	if !id.IsSSD() {
		t.Error("Samsung 850 PRO should be SSD")
	}
}

func TestRealDrive_ADATASU650(t *testing.T) {
	// ADATA SU650: unusual - SSD that reports APM support
	raw := buildIdentifyData(identifyOpts{
		model:   "ADATA SU650",
		word83:  0x7509,
		word86:  0xB409,
		word91:  0x0080,
		word217: 0x0001,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("ADATA SU650 should support APM")
	}
	if !id.IsAPMEnabled() {
		t.Error("ADATA SU650 should have APM enabled")
	}
	if id.CurrentAPMLevel() != 128 {
		t.Errorf("ADATA SU650 APM level = %d, want 128", id.CurrentAPMLevel())
	}
	if !id.IsSSD() {
		t.Error("ADATA SU650 should be SSD")
	}
}

func TestRealDrive_SeagateST4000(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "ST4000DM005-2DP166",
		word83:  0x7D6B,
		word86:  0xBC41,
		word217: 5980,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("Seagate ST4000 should support APM")
	}
	if id.IsAPMEnabled() {
		t.Error("Seagate ST4000 should have APM disabled")
	}
	if id.IsSSD() {
		t.Error("Seagate ST4000 should not be SSD")
	}
}

func TestRealDrive_HGST(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "HGST HTS721010A9E630",
		word83:  0x7D69,
		word86:  0xBC41,
		word217: 7200,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("HGST should support APM")
	}
	if id.IsSSD() {
		t.Error("HGST should not be SSD")
	}
}

func TestRealDrive_ToshibaMQ01(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "TOSHIBA MQ01ABD100",
		word83:  0x7D69,
		word86:  0xBC41,
		word91:  0x0080,
		word217: 5400,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("Toshiba MQ01 should support APM")
	}
	if id.IsSSD() {
		t.Error("Toshiba should not be SSD")
	}
	if id.RotationRate() != 5400 {
		t.Errorf("Toshiba rotation = %d, want 5400", id.RotationRate())
	}
}

func TestRealDrive_WDBlue(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "WDC WD10JPVX-75JC3T0",
		word83:  0x7D69,
		word86:  0xBC41,
		word91:  0x00FE,
		word217: 5400,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("WD Blue should support APM")
	}
	if id.Model() != "WDC WD10JPVX-75JC3T0" {
		t.Errorf("Model = %q", id.Model())
	}
}

func TestRealDrive_SeagateST5000LM(t *testing.T) {
	raw := buildIdentifyData(identifyOpts{
		model:   "ST5000LM000-2AN170",
		word83:  0x7D6B,
		word86:  0xBC41,
		word217: 5526,
	})
	id := ata.ParseIdentify(raw)

	if !id.IsAPMSupported() {
		t.Error("ST5000LM should support APM")
	}
	if id.IsSSD() {
		t.Error("ST5000LM should not be SSD")
	}
}

// =====================================================================
// DiskInfo and APMResult zero values
// =====================================================================

func TestDiskInfo_ZeroValue(t *testing.T) {
	var d ata.DiskInfo
	if d.APMSupported {
		t.Error("zero DiskInfo should not be APM supported")
	}
	if d.IsSSDDrive {
		t.Error("zero DiskInfo should not be SSD")
	}
}

func TestAPMResult_ZeroValue(t *testing.T) {
	var r ata.APMResult
	if r.Success {
		t.Error("zero APMResult should not be successful")
	}
	if r.Error != nil {
		t.Error("zero APMResult should have nil error")
	}
}
