//go:build desktop && darwin

package main

// Wails 2.15 uses UTType; link its framework explicitly on modern macOS SDKs.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
