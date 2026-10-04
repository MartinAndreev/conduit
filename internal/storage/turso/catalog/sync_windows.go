//go:build windows

package catalog

// Windows has no portable directory fsync through os.File. The database file is
// synced before publication; namespace durability through power loss is unverified.
func syncCatalogDirectory(string) error { return nil }
