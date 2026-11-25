//go:build windows

package app

import (
	"golang.org/x/sys/windows"
)

func diskSpace(path string) (total, free uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeBytes, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeBytes, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return totalBytes, freeBytes, nil
}
