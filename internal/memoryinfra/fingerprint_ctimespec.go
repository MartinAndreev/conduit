//go:build darwin || freebsd || netbsd

package memoryinfra

import "syscall"

// changeTimeOf reads the change time where syscall names it Ctimespec.
func changeTimeOf(stat *syscall.Stat_t) int64 {
	return int64(stat.Ctimespec.Sec)*1e9 + int64(stat.Ctimespec.Nsec)
}
