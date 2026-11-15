//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package memoryinfra

import (
	"io/fs"
	"syscall"
)

// nativeStat is the Unix part of a file's fingerprint: device and inode
// (a replaced file differs even with equal size and times) and the change
// time, which every write, chmod or rename updates and no user call sets.
type nativeStat struct {
	dev   uint64
	ino   uint64
	ctime int64 // nanoseconds since the Unix epoch
}

func nativeStatOf(info fs.FileInfo) (nativeStat, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return nativeStat{}, false
	}
	return nativeStat{dev: uint64(stat.Dev), ino: uint64(stat.Ino), ctime: changeTimeOf(stat)}, true
}

// changeTime is the change time; the racy rule checks it with mtime.
func (n nativeStat) changeTime() int64 { return n.ctime }
