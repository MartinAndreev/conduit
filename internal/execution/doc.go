// Package execution owns the dispatch intent of spec 003: its identities,
// states and the pure rules that decide whether a StartTurn or Steer command
// may be recorded, claimed for sending and how its observed outcome is
// classified. It performs no I/O.
//
// The order is: a transaction records the intent (with the context manifest
// and its payload when memory was composed), a second transaction claims it,
// the harness is called outside any transaction, and a third transaction
// records the outcome. An effect whose outcome is unknown is never repeated
// automatically. This is a library slice without operational recovery: a
// session with an open intent (recorded, dispatching, unknown, diverged)
// accepts no new intent and no binding change until a future recovery
// procedure releases it.
//
// sessions.Record.Revision serves as the assignment revision. That is
// explicitly temporary (spec 003, decision of 2026-11-16); it advances only
// through an accepted, correlated Steer whose adapter proved native receipt.
package execution
