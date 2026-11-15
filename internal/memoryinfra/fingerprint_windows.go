//go:build windows

package memoryinfra

import (
	"io/fs"
	"math"
	"syscall"
)

// nativeStat is the Windows part of a file's fingerprint: the creation time
// from the attribute data os.Lstat returns. It is weaker than the Unix one:
// without a file ID a file replaced with equal size, attributes and times is
// not noticed, and without a change time the racy rule checks only the
// modification time.
type nativeStat struct {
	creation int64 // nanoseconds since the Unix epoch
}

func nativeStatOf(info fs.FileInfo) (nativeStat, bool) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data == nil {
		return nativeStat{}, false
	}
	return nativeStat{creation: data.CreationTime.Nanoseconds()}, true
}

// changeTime: Windows has no change time here, so only mtime is checked.
func (n nativeStat) changeTime() int64 { return math.MinInt64 }
