//go:build windows

package storage

import (
	"syscall"
	"unsafe"
)

var crossDeviceError = syscall.EXDEV

func diskUsage(path string) (total, used, free int64, err error) {
	kernel32 := syscall.MustLoadDLL("kernel32.dll")
	getDiskFreeSpace := kernel32.MustFindProc("GetDiskFreeSpaceExW")

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}

	var freeBytes, totalBytes, totalFreeBytes int64
	ret, _, callErr := getDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		err = callErr
		return
	}
	total = totalBytes
	free = freeBytes
	used = total - free
	return
}
