// Package turso implements Conduit's persistence ports with the embedded Turso
// Database engine. SQL connections and transactions do not cross this package
// boundary. Open creates fresh databases at the current schema and migrates
// schema v1 additively to v2; other versions are refused.
//
// Another process holding the file is reported as ErrDatabaseLocked. With
// Options.MultiprocessWAL, processes opening together before <db>-tshm exists
// race on creating it and the loser's open fails as engine corruption. Only
// that failure, and only with the option on, is retried: at most 5 attempts
// with 10-40ms waits. If it persists, Open returns ErrSharedMemoryInit, never
// ErrIncompatibleDatabase.
package turso
