// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build linux

package ata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux ioctl numbers for ATA commands.
const (
	hdioGetIdentityIO = 0x030D // HDIO_GET_IDENTITY
	hdioDriveCmdIO    = 0x031F // HDIO_DRIVE_CMD
	sgioIO            = 0x2285 // SG_IO

	ataCmdIdentify    = 0xEC // ATA IDENTIFY DEVICE
	ataCmdSetFeatures = 0xEF // ATA SET FEATURES

	ataFeatureEnableAPM  = 0x05 // SET FEATURES subcommand: enable APM
	ataFeatureDisableAPM = 0x85 // SET FEATURES subcommand: disable APM

	satPassthrough16 = 0x85 // SAT ATA PASS-THROUGH(16) SCSI opcode

	sgDxferNone    = -1
	sgDxferFromDev = -3
)

// sgIOHdr is the Linux sg_io_hdr_t structure passed to SG_IO ioctl.
type sgIOHdr struct {
	interfaceID    int32
	dxferDirection int32
	cmdLen         uint8
	mxSbLen        uint8
	iovecCount     uint16
	dxferLen       uint32
	dxferp         uintptr
	cmdp           uintptr
	sbp            uintptr
	timeout        uint32
	flags          uint32
	packID         int32
	usrPtr         uintptr
	status         uint8
	maskedStatus   uint8
	msgStatus      uint8
	sbLenWr        uint8
	hostStatus     uint16
	driverStatus   uint16
	resid          int32
	duration       uint32
	info           uint32
}

// EnumerateDisks discovers all SATA/USB-attached block devices on Linux.
// NVMe paths (/dev/nvme*) are not included; they are handled separately.
func EnumerateDisks() ([]string, error) {
	var disks []string

	patterns := []string{
		"/dev/sd[a-z]",
		"/dev/sd[a-z][a-z]",
		"/dev/hd[a-z]",
	}
	for _, p := range patterns {
		m, _ := filepath.Glob(p)
		disks = append(disks, m...)
	}

	var valid []string
	for _, d := range disks {
		info, err := os.Stat(d)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeDevice != 0 {
			valid = append(valid, d)
		}
	}
	return valid, nil
}

// IdentifyDevice sends ATA IDENTIFY DEVICE to path and returns the raw 512-byte
// response. Three methods are tried in order: HDIO_GET_IDENTITY, HDIO_DRIVE_CMD,
// and SG_IO SAT PASS-THROUGH (for USB-attached drives).
// O_NONBLOCK is required so that opening a block device does not block waiting
// for media; HDIO_DRIVE_CMD also requires it on some kernels.
func IdentifyDevice(path string) ([]byte, error) {
	fd, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer fd.Close()

	if data, err := hdioGetIdentityCmd(fd); err == nil {
		return data, nil
	}
	if data, err := hdioDriveCmd(fd, ataCmdIdentify, 0, 0, 1); err == nil {
		return data, nil
	}
	if data, err := sgioIdentify(fd); err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("all identify methods failed for %s", path)
}

// SetAPM sets the APM level on the device at path.
// level 1-254 enables APM at that value; level 255 disables APM entirely.
func SetAPM(path string, level uint8) error {
	fd, err := os.OpenFile(path, os.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open %s for write: %w", path, err)
	}
	defer fd.Close()

	var feature, sectorCount uint8
	if level == APMDisable {
		feature = ataFeatureDisableAPM
		sectorCount = 0
	} else {
		feature = ataFeatureEnableAPM
		sectorCount = level
	}

	if err := setAPMviaDriveCmd(fd, feature, sectorCount); err == nil {
		return nil
	}
	return setAPMviaSGIO(fd, feature, sectorCount)
}

// DetectInterface returns the connection bus type for a device path.
// Returns one of: "SATA", "USB/SATA", "NVMe", or "Unknown".
func DetectInterface(path string) string {
	devName := filepath.Base(path)
	if strings.HasPrefix(devName, "nvme") {
		return "NVMe"
	}
	// Check sysfs driver link for USB involvement.
	syspath := fmt.Sprintf("/sys/block/%s/device/../../driver", devName)
	if link, err := os.Readlink(syspath); err == nil && strings.Contains(link, "usb") {
		return "USB/SATA"
	}
	// Alternative: evaluate the sysfs symlink path itself.
	if target, err := filepath.EvalSymlinks(fmt.Sprintf("/sys/block/%s", devName)); err == nil {
		if strings.Contains(target, "/usb") {
			return "USB/SATA"
		}
		if strings.Contains(target, "/nvme") {
			return "NVMe"
		}
	}
	return "SATA"
}

// IsNVMeDevice reports whether the device path refers to an NVMe device.
func IsNVMeDevice(path string) bool {
	return strings.Contains(filepath.Base(path), "nvme")
}

// hdioGetIdentityCmd reads drive identity via HDIO_GET_IDENTITY ioctl.
func hdioGetIdentityCmd(fd *os.File) ([]byte, error) {
	buf := make([]byte, 512)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), hdioGetIdentityIO, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil, fmt.Errorf("HDIO_GET_IDENTITY: %v", errno)
	}
	return buf, nil
}

// hdioDriveCmd sends an arbitrary ATA command via HDIO_DRIVE_CMD.
// Returns the data payload (excluding the 4-byte command header).
func hdioDriveCmd(fd *os.File, command, nsector, feature uint8, dataBlocks uint8) ([]byte, error) {
	buf := make([]byte, 4+int(dataBlocks)*512)
	buf[0] = command
	buf[1] = nsector
	buf[2] = feature
	buf[3] = dataBlocks
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), hdioDriveCmdIO, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil, fmt.Errorf("HDIO_DRIVE_CMD(0x%02X): %v", command, errno)
	}
	if dataBlocks > 0 {
		return buf[4:], nil
	}
	return buf[:4], nil
}

// sgioIdentify sends ATA IDENTIFY DEVICE via SG_IO (SAT ATA PASS-THROUGH 16).
// This path is used for USB-attached drives that bridge via SCSI.
func sgioIdentify(fd *os.File) ([]byte, error) {
	data := make([]byte, 512)
	sense := make([]byte, 32)
	cdb := [16]byte{
		satPassthrough16,
		(4 << 1), // protocol=PIO Data-In (4<<1 = 0x08)
		0x0E,     // T_DIR=1(from dev), BYTE_BLOCK=1, T_LENGTH=10(sector-count), CK_COND=0
		0, 0,     // features high/low
		0, 1, // sector count high/low = 1
		0, 0, 0, 0, 0, 0, 0, // LBA
		ataCmdIdentify,
		0, // control
	}
	hdr := sgIOHdr{
		interfaceID:    int32('S'),
		dxferDirection: sgDxferFromDev,
		cmdLen:         16,
		mxSbLen:        uint8(len(sense)),
		dxferLen:       512,
		dxferp:         uintptr(unsafe.Pointer(&data[0])),
		cmdp:           uintptr(unsafe.Pointer(&cdb[0])),
		sbp:            uintptr(unsafe.Pointer(&sense[0])),
		timeout:        5000,
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), sgioIO, uintptr(unsafe.Pointer(&hdr)))
	if errno != 0 {
		return nil, fmt.Errorf("SG_IO IDENTIFY: %v", errno)
	}
	if hdr.status != 0 && hdr.status != 2 {
		return nil, fmt.Errorf("SG_IO IDENTIFY: SCSI status 0x%02X", hdr.status)
	}
	for _, b := range data {
		if b != 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("SG_IO IDENTIFY returned empty data")
}

// setAPMviaDriveCmd uses HDIO_DRIVE_CMD to issue ATA SET FEATURES.
func setAPMviaDriveCmd(fd *os.File, feature uint8, sectorCount uint8) error {
	buf := [4]byte{ataCmdSetFeatures, sectorCount, feature, 0}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), hdioDriveCmdIO, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return fmt.Errorf("HDIO_DRIVE_CMD SET_FEATURES(0x%02X): %v", feature, errno)
	}
	return nil
}

// setAPMviaSGIO uses SG_IO SAT PASS-THROUGH to issue ATA SET FEATURES.
// Used for USB-attached drives.
func setAPMviaSGIO(fd *os.File, feature uint8, sectorCount uint8) error {
	sense := make([]byte, 32)
	cdb := [16]byte{
		satPassthrough16,
		(3 << 1), // protocol=Non-data (3<<1 = 0x06)
		0x00,     // T_LENGTH=00 (no transfer), CK_COND=0
		0, feature,
		0, sectorCount,
		0, 0, 0, 0, 0, 0, 0,
		ataCmdSetFeatures,
		0,
	}
	hdr := sgIOHdr{
		interfaceID:    int32('S'),
		dxferDirection: sgDxferNone,
		cmdLen:         16,
		mxSbLen:        uint8(len(sense)),
		cmdp:           uintptr(unsafe.Pointer(&cdb[0])),
		sbp:            uintptr(unsafe.Pointer(&sense[0])),
		timeout:        5000,
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd.Fd(), sgioIO, uintptr(unsafe.Pointer(&hdr)))
	if errno != 0 {
		return fmt.Errorf("SG_IO SET_FEATURES(0x%02X): %v", feature, errno)
	}
	if hdr.status != 0 {
		return fmt.Errorf("SG_IO SET_FEATURES: SCSI status 0x%02X", hdr.status)
	}
	return nil
}
