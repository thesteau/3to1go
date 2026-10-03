//go:build !windows

package storage

import (
	"syscall"
)

var crossDeviceError = syscall.EXDEV

func diskUsage(path string) (total, used, free int64, err error) {
	var stat syscall.Statfs_t
	if err = syscall.Statfs(path, &stat); err != nil {
		return
	}
	total = int64(stat.Blocks) * int64(stat.Bsize)
	free = int64(stat.Bfree) * int64(stat.Bsize)
	used = total - free
	return
}
