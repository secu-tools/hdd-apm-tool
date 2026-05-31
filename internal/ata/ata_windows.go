// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package ata

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows ioctl codes.
const (
	ioctlATAPassThrough        = 0x4D02C // IOCTL_ATA_PASS_THROUGH
	ioctlSCSIPassThrough       = 0x4D004 // IOCTL_SCSI_PASS_THROUGH (buffered)
	ioctlSCSIPassThroughDirect = 0x4D014 // IOCTL_SCSI_PASS_THROUGH_DIRECT (for USB)
	ioctlStorageQueryProperty  = 0x2D1400

	ataCmdIdentify    = 0xEC
	ataCmdSetFeatures = 0xEF

	ataFeatureEnableAPM  = 0x05
	ataFeatureDisableAPM = 0x85

	ataFlagsDataIn       = 0x02
	ataFlagsDRDYRequired = 0x01

	satPassthrough12 = 0xA1 // ATA PASS-THROUGH(12)
	satPassthrough16 = 0x85 // ATA PASS-THROUGH(16) - wider USB bridge support

	maxPhysicalDrives = 64

	busTypeAta  = 3
	busTypeSata = 11
	busTypeNvme = 17
	busTypeUsb  = 7
	busTypeScsi = 1
)

// ataPassThroughEx is the Windows ATA_PASS_THROUGH_EX structure.
type ataPassThroughEx struct {
	Length             uint16
	AtaFlags           uint16
	PathId             uint8
	TargetId           uint8
	Lun                uint8
	ReservedAsUchar    uint8
	DataTransferLength uint32
	TimeOutValue       uint32
	ReservedAsUlong    uint32
	DataBufferOffset   uintptr
	PreviousTaskFile   [8]uint8
	CurrentTaskFile    [8]uint8
}

type ataPassThroughWithBuffer struct {
	Header ataPassThroughEx
	Data   [512]byte
}

// scsiPassThrough is the Windows SCSI_PASS_THROUGH structure.
type scsiPassThrough struct {
	Length             uint16
	ScsiStatus         uint8
	PathId             uint8
	TargetId           uint8
	Lun                uint8
	CdbLength          uint8
	SenseInfoLength    uint8
	DataIn             uint8
	_pad1              [3]uint8
	DataTransferLength uint32
	TimeOutValue       uint32
	DataBufferOffset   uintptr
	SenseInfoOffset    uint32
	Cdb                [16]uint8
}

type scsiPassThroughWithBuffers struct {
	Spt   scsiPassThrough
	Sense [32]byte
	Data  [512]byte
}

// scsiPassThroughDirect is the Windows SCSI_PASS_THROUGH_DIRECT structure.
// DataBuffer is an unsafe.Pointer so the GC tracks the referenced buffer and
// keeps it alive during the kernel call. Layout matches the Windows SDK struct
// on both x64 and arm64 (pointer-sized DataBuffer, aligned at offset 24).
type scsiPassThroughDirect struct {
	Length             uint16
	ScsiStatus         uint8
	PathId             uint8
	TargetId           uint8
	Lun                uint8
	CdbLength          uint8
	SenseInfoLength    uint8
	DataIn             uint8
	_pad1              [3]uint8
	DataTransferLength uint32
	TimeOutValue       uint32
	DataBuffer         unsafe.Pointer // direct pointer to user-space data buffer
	SenseInfoOffset    uint32
	Cdb                [16]uint8
}

type scsiPassThroughDirectWithSense struct {
	Spt   scsiPassThroughDirect
	Sense [32]byte
}

// storagePropertyQuery is used with IOCTL_STORAGE_QUERY_PROPERTY.
type storagePropertyQuery struct {
	PropertyId uint32
	QueryType  uint32
	Additional [1]byte
}

// storageDeviceDescriptor (partial) gives us the bus type.
type storageDeviceDescriptor struct {
	Version               uint32
	Size                  uint32
	DeviceType            byte
	DeviceTypeModifier    byte
	RemovableMedia        byte
	CommandQueueing       byte
	VendorIdOffset        uint32
	ProductIdOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               uint32
	RawPropertiesLength   uint32
	RawDeviceProperties   [1]byte
}

// EnumerateDisks discovers all physical drives on Windows.
func EnumerateDisks() ([]string, error) {
	var disks []string
	for i := 0; i < maxPhysicalDrives; i++ {
		path := fmt.Sprintf(`\\.\PhysicalDrive%d`, i)
		ptr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		h, err := windows.CreateFile(ptr,
			windows.GENERIC_READ,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		windows.CloseHandle(h)
		disks = append(disks, path)
	}
	return disks, nil
}

// IdentifyDevice sends ATA IDENTIFY DEVICE to path and returns the raw 512-byte
// response. Tries ATA_PASS_THROUGH first (direct SATA), then SCSI SAT12 buffered
// passthrough, then SCSI SAT16 direct passthrough (required for many USB bridges).
func IdentifyDevice(path string) ([]byte, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer windows.CloseHandle(h)

	// Method 1: ATA pass-through (direct SATA/AHCI)
	if data, err := ataPassThroughIdentify(h); err == nil {
		return data, nil
	}
	// Method 2: SCSI SAT12 buffered (some SATA/USB bridges)
	if data, err := scsiSATIdentify(h); err == nil {
		return data, nil
	}
	// Method 3: SCSI SAT16 direct (IOCTL_SCSI_PASS_THROUGH_DIRECT - required for
	// USB Mass Storage bridges that do not support the buffered IOCTL)
	if data, err := scsiSATIdentifyDirect(h); err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("all identify methods failed for %s", path)
}

// SetAPM sets the APM level on the device at path. Mirrors the same three-method
// fallback chain as IdentifyDevice.
func SetAPM(path string, level uint8) error {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fmt.Errorf("open %s for write: %w", path, err)
	}
	defer windows.CloseHandle(h)

	if err := ataPassThroughSetAPM(h, level); err == nil {
		return nil
	}
	if err := scsiSATSetAPM(h, level); err == nil {
		return nil
	}
	return scsiSATSetAPMDirect(h, level)
}

// DetectInterface returns the bus type for a Windows physical drive path.
func DetectInterface(path string) string {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "Unknown"
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "Unknown"
	}
	defer windows.CloseHandle(h)

	query := storagePropertyQuery{PropertyId: 0, QueryType: 0}
	outBuf := make([]byte, 1024)
	var ret uint32
	err = windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		&outBuf[0], uint32(len(outBuf)), &ret, nil)
	if err != nil || ret < 32 {
		return "Unknown"
	}
	desc := (*storageDeviceDescriptor)(unsafe.Pointer(&outBuf[0]))
	switch desc.BusType {
	case busTypeAta, busTypeSata:
		return "SATA"
	case busTypeNvme:
		return "NVMe"
	case busTypeUsb:
		return "USB/SATA"
	case busTypeScsi:
		return "SCSI/SAS"
	default:
		return fmt.Sprintf("Bus(%d)", desc.BusType)
	}
}

// IsNVMeDevice reports whether the device is an NVMe drive.
func IsNVMeDevice(path string) bool {
	return strings.Contains(DetectInterface(path), "NVMe")
}

// ataPassThroughIdentify sends IDENTIFY DEVICE via IOCTL_ATA_PASS_THROUGH.
func ataPassThroughIdentify(h windows.Handle) ([]byte, error) {
	var buf ataPassThroughWithBuffer
	buf.Header.Length = uint16(unsafe.Sizeof(buf.Header))
	buf.Header.AtaFlags = ataFlagsDataIn | ataFlagsDRDYRequired
	buf.Header.DataTransferLength = 512
	buf.Header.TimeOutValue = 5
	buf.Header.DataBufferOffset = unsafe.Offsetof(buf.Data)
	buf.Header.CurrentTaskFile[1] = 1              // SectorCount
	buf.Header.CurrentTaskFile[5] = 0xA0           // Device/Head (master)
	buf.Header.CurrentTaskFile[6] = ataCmdIdentify // Command

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlATAPassThrough,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return nil, fmt.Errorf("IOCTL_ATA_PASS_THROUGH IDENTIFY: %w", err)
	}
	for _, b := range buf.Data {
		if b != 0 {
			return buf.Data[:], nil
		}
	}
	return nil, fmt.Errorf("IDENTIFY returned empty data")
}

// scsiSATIdentify sends IDENTIFY DEVICE via SCSI_PASS_THROUGH (SAT12).
func scsiSATIdentify(h windows.Handle) ([]byte, error) {
	var buf scsiPassThroughWithBuffers
	buf.Spt.Length = uint16(unsafe.Sizeof(buf.Spt))
	buf.Spt.CdbLength = 12
	buf.Spt.SenseInfoLength = uint8(len(buf.Sense))
	buf.Spt.DataIn = 1 // SCSI_IOCTL_DATA_IN
	buf.Spt.DataTransferLength = 512
	buf.Spt.TimeOutValue = 10
	buf.Spt.DataBufferOffset = unsafe.Offsetof(buf.Data)
	buf.Spt.SenseInfoOffset = uint32(unsafe.Offsetof(buf.Sense))
	// SAT12 ATA PASS-THROUGH(12) CDB for IDENTIFY DEVICE
	buf.Spt.Cdb[0] = satPassthrough12
	buf.Spt.Cdb[1] = (4 << 1) // Protocol = PIO Data-In
	buf.Spt.Cdb[2] = 0x0E     // T_DIR=device->host, BYTE_BLOCK, T_LENGTH=sector-count
	buf.Spt.Cdb[4] = 1        // sector count
	buf.Spt.Cdb[8] = 0xA0     // device (master)
	buf.Spt.Cdb[9] = ataCmdIdentify

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlSCSIPassThrough,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return nil, fmt.Errorf("SCSI_PASS_THROUGH SAT12 IDENTIFY: %w", err)
	}
	if buf.Spt.ScsiStatus != 0 {
		return nil, fmt.Errorf("SAT12 IDENTIFY: SCSI status 0x%02X", buf.Spt.ScsiStatus)
	}
	for _, b := range buf.Data {
		if b != 0 {
			return buf.Data[:], nil
		}
	}
	return nil, fmt.Errorf("SAT12 IDENTIFY: empty response")
}

// ataPassThroughSetAPM sets APM via IOCTL_ATA_PASS_THROUGH.
func ataPassThroughSetAPM(h windows.Handle, level uint8) error {
	var buf ataPassThroughWithBuffer
	buf.Header.Length = uint16(unsafe.Sizeof(buf.Header))
	buf.Header.AtaFlags = ataFlagsDRDYRequired
	buf.Header.DataTransferLength = 0
	buf.Header.TimeOutValue = 5
	buf.Header.DataBufferOffset = unsafe.Offsetof(buf.Data)
	if level == APMDisable {
		buf.Header.CurrentTaskFile[0] = ataFeatureDisableAPM
		buf.Header.CurrentTaskFile[1] = 0
	} else {
		buf.Header.CurrentTaskFile[0] = ataFeatureEnableAPM
		buf.Header.CurrentTaskFile[1] = level
	}
	buf.Header.CurrentTaskFile[5] = 0xA0
	buf.Header.CurrentTaskFile[6] = ataCmdSetFeatures

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlATAPassThrough,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return fmt.Errorf("IOCTL_ATA_PASS_THROUGH SET_FEATURES: %w", err)
	}
	if buf.Header.CurrentTaskFile[0] != 0 {
		return fmt.Errorf("SET_FEATURES error register: 0x%02X", buf.Header.CurrentTaskFile[0])
	}
	return nil
}

// scsiSATSetAPM sets APM via SCSI_PASS_THROUGH (SAT12 non-data).
func scsiSATSetAPM(h windows.Handle, level uint8) error {
	var buf scsiPassThroughWithBuffers
	buf.Spt.Length = uint16(unsafe.Sizeof(buf.Spt))
	buf.Spt.CdbLength = 12
	buf.Spt.SenseInfoLength = uint8(len(buf.Sense))
	buf.Spt.DataIn = 0 // non-data command
	buf.Spt.DataTransferLength = 0
	buf.Spt.TimeOutValue = 10
	buf.Spt.DataBufferOffset = unsafe.Offsetof(buf.Data)
	buf.Spt.SenseInfoOffset = uint32(unsafe.Offsetof(buf.Sense))

	var feature, sectorCount uint8
	if level == APMDisable {
		feature = ataFeatureDisableAPM
	} else {
		feature = ataFeatureEnableAPM
		sectorCount = level
	}
	// SAT12 ATA PASS-THROUGH(12) CDB for SET FEATURES (non-data)
	buf.Spt.Cdb[0] = satPassthrough12
	buf.Spt.Cdb[1] = (3 << 1) // Protocol = Non-data
	buf.Spt.Cdb[2] = 0x00     // no data transfer
	buf.Spt.Cdb[3] = feature
	buf.Spt.Cdb[4] = sectorCount
	buf.Spt.Cdb[8] = 0xA0
	buf.Spt.Cdb[9] = ataCmdSetFeatures

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlSCSIPassThrough,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return fmt.Errorf("SCSI_PASS_THROUGH SAT12 SET_FEATURES: %w", err)
	}
	if buf.Spt.ScsiStatus != 0 {
		return fmt.Errorf("SAT12 SET_FEATURES: SCSI status 0x%02X", buf.Spt.ScsiStatus)
	}
	return nil
}

// scsiSATIdentifyDirect sends IDENTIFY DEVICE via IOCTL_SCSI_PASS_THROUGH_DIRECT
// with a SAT16 CDB. This IOCTL is required for USB Mass Storage devices whose
// host-controller driver does not support the buffered SCSI_PASS_THROUGH variant.
func scsiSATIdentifyDirect(h windows.Handle) ([]byte, error) {
	data := make([]byte, 512)
	var buf scsiPassThroughDirectWithSense
	buf.Spt.Length = uint16(unsafe.Sizeof(buf.Spt))
	buf.Spt.CdbLength = 16
	buf.Spt.SenseInfoLength = uint8(len(buf.Sense))
	buf.Spt.DataIn = 1 // SCSI_IOCTL_DATA_IN
	buf.Spt.DataTransferLength = 512
	buf.Spt.TimeOutValue = 10
	buf.Spt.DataBuffer = unsafe.Pointer(&data[0])
	buf.Spt.SenseInfoOffset = uint32(unsafe.Offsetof(buf.Sense))
	// SAT16 ATA PASS-THROUGH(16) CDB for IDENTIFY DEVICE
	buf.Spt.Cdb[0] = satPassthrough16
	buf.Spt.Cdb[1] = (4 << 1) // Protocol = PIO Data-In
	buf.Spt.Cdb[2] = 0x0E     // T_DIR=device->host, BYTE_BLOCK, T_LENGTH=sector-count
	buf.Spt.Cdb[6] = 1        // sector count [7:0]
	buf.Spt.Cdb[13] = 0xA0    // device (master)
	buf.Spt.Cdb[14] = ataCmdIdentify

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlSCSIPassThroughDirect,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return nil, fmt.Errorf("SCSI_PASS_THROUGH_DIRECT SAT16 IDENTIFY: %w", err)
	}
	if buf.Spt.ScsiStatus != 0 {
		return nil, fmt.Errorf("SPTD SAT16 IDENTIFY: SCSI status 0x%02X", buf.Spt.ScsiStatus)
	}
	for _, b := range data {
		if b != 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("SPTD SAT16 IDENTIFY: empty response")
}

// scsiSATSetAPMDirect sets APM via IOCTL_SCSI_PASS_THROUGH_DIRECT (SAT16 non-data).
func scsiSATSetAPMDirect(h windows.Handle, level uint8) error {
	var buf scsiPassThroughDirectWithSense
	buf.Spt.Length = uint16(unsafe.Sizeof(buf.Spt))
	buf.Spt.CdbLength = 16
	buf.Spt.SenseInfoLength = uint8(len(buf.Sense))
	buf.Spt.DataIn = 0 // non-data command
	buf.Spt.DataTransferLength = 0
	buf.Spt.TimeOutValue = 10
	buf.Spt.DataBuffer = nil
	buf.Spt.SenseInfoOffset = uint32(unsafe.Offsetof(buf.Sense))

	var feature, sectorCount uint8
	if level == APMDisable {
		feature = ataFeatureDisableAPM
	} else {
		feature = ataFeatureEnableAPM
		sectorCount = level
	}
	// SAT16 ATA PASS-THROUGH(16) CDB for SET FEATURES (non-data)
	buf.Spt.Cdb[0] = satPassthrough16
	buf.Spt.Cdb[1] = (3 << 1)    // Protocol = Non-data
	buf.Spt.Cdb[2] = 0x00        // no data transfer
	buf.Spt.Cdb[4] = feature     // features [7:0]
	buf.Spt.Cdb[6] = sectorCount // count [7:0] = APM level
	buf.Spt.Cdb[13] = 0xA0
	buf.Spt.Cdb[14] = ataCmdSetFeatures

	var ret uint32
	if err := windows.DeviceIoControl(h, ioctlSCSIPassThroughDirect,
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		(*byte)(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)),
		&ret, nil); err != nil {
		return fmt.Errorf("SCSI_PASS_THROUGH_DIRECT SAT16 SET_FEATURES: %w", err)
	}
	if buf.Spt.ScsiStatus != 0 {
		return fmt.Errorf("SPTD SAT16 SET_FEATURES: SCSI status 0x%02X", buf.Spt.ScsiStatus)
	}
	return nil
}
