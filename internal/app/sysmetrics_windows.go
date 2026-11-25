//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

// The pinned golang.org/x/sys version does not expose GlobalMemoryStatusEx or
// GetSystemTimes, so the two Win32 calls are made directly here.

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
)

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

type filetime struct {
	lowDateTime  uint32
	highDateTime uint32
}

// memUsage reports used and total physical memory in bytes.
func memUsage() (used, total uint64, ok bool) {
	var ms memoryStatusEx
	ms.length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0, 0, false
	}
	total = ms.totalPhys
	avail := ms.availPhys
	if avail > total {
		avail = total
	}
	return total - avail, total, true
}

// cpuTimes returns cumulative idle and total 100ns ticks via GetSystemTimes.
// Kernel time includes idle time, so total = kernel + user.
func cpuTimes() (idle, total uint64, ok bool) {
	var idleTime, kernelTime, userTime filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if r == 0 {
		return 0, 0, false
	}
	return filetimeTicks(idleTime), filetimeTicks(kernelTime) + filetimeTicks(userTime), true
}

func filetimeTicks(ft filetime) uint64 {
	return uint64(ft.highDateTime)<<32 | uint64(ft.lowDateTime)
}
