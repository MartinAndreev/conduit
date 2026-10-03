//go:build windows

package startupinfra

// Windows cannot portably sync a directory using os.File.Sync. File contents
// are synced before publication; namespace durability through power loss is not promised.
func syncDirectory(string) error { return nil }
