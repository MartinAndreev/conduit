# Harness provider contract package

Scope approved on 2026-09-21. This package is the consumer-owned Go contract
between Conduit sessions and agent-harness adapters. `Provider` means a harness
adapter, not an inference API provider. Concrete Codex/OpenCode adapters belong
under `internal/harnesses` in later approved work.

The reviewed design snapshot is
[`11-provider-contract.md`](../../../../conduit-re-write-specs/11-provider-contract.md).
This README records the implementation scope so the sibling guide is not the
only contract reference.

## Ownership

- The adapter owns native protocol communication, correlation, capability
  observations, and normalized native events.
- Conduit owns assignments and revisions, durable control intent, queue and
  interruption policy, verification, result acceptance, and completion.
- A result event is an untrusted candidate. Native idle, terminal state, valid
  JSON, or a `completed` field cannot complete a Conduit task.
- Valid JSON in assistant commentary remains a text event. Candidate sources are
  explicit; unresolved candidate attribution remains unknown rather than assuming
  the latest assignment revision.
- Native IDs only correlate transport activity. They never replace Conduit IDs.

## Semantics preserved by the types

- Capability support and evidence level are separate, and unknown stays unknown.
- Schema fallback is caller-listed and the actual enforcement mode is returned.
- Active-turn and next-boundary steering are distinct explicit delivery modes.
- Steering carries the current reconciled assignment and the proposed new
  revision. Unknown delivery does not attribute later candidates to that revision.
- A native receipt is separate from verified application of a correction.
- Permission answers, question answers, interruption, and close are separate calls.
- RPC context cancellation stops waiting; it does not imply remote cancellation.
- Request IDs support reconciliation, not a universal exactly-once guarantee.
- Event bodies form a closed typed union. Text updates carry item, segment,
  version, byte-offset, and truncation identity. A truncated update creates a gap;
  later deltas cannot be spliced over its missing bytes. Optional native values
  distinguish unknown from a known zero.
- Adapter sequence/cursors are not durable run ordering or replay. Those remain
  responsibilities of Conduit's journal.
- Primary and auxiliary model usage are separately scoped when exposed. Each
  usage observation says whether counters are a delta or cumulative snapshot and
  carries a scope ID, so downstream code does not sum the same total twice.

## Explicitly outside this package

Transport implementations, process supervision, orchestration, persistence,
retry decisions, durable replay, revision transactions, schema/semantic result
validation, independent verification, completion gates, and UI rendering.

## Acceptance checks

```sh
gofmt -w internal/sessions/provider
go test ./...
go vet ./...
go build ./...
go run ./cmd/conduit --help
```

The boundary tests use external-package fakes to prove adapters and consumers can
implement and call the interfaces without access to package internals.
