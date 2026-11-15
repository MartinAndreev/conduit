//go:build aix || dragonfly || linux || openbsd || solaris

package memoryinfra

import "syscall"

// changeTimeOf reads the change time where syscall names it Ctim.
func changeTimeOf(stat *syscall.Stat_t) int64 {
	return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec)
}
