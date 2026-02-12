//go:build !windows

package tailf

import (
	"os"
	"syscall"
)

type fileIdentity struct {
	dev uint64
	ino uint64
}

func getFileIdentity(info os.FileInfo) fileIdentity {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}
	}
	return fileIdentity{
		dev: uint64(stat.Dev),
		ino: uint64(stat.Ino),
	}
}
