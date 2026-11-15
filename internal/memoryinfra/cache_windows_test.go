//go:build windows

package memoryinfra

func otherNativeStat() nativeStat { return nativeStat{creation: 1} }
