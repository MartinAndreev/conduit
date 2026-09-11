# Feature 007 contracts

## Implemented and frozen

- `agent-assignment-v1.schema.json`: universal role assignment.
- `agent-response-v1.schema.json`: universal final role response, including bounded memory proposals.
- `conduit-result-record-v1.schema.json`: persisted Conduit wrapper with authoritative metadata.

The long-term memory extension preserves these three schemas byte-for-byte.

## Proposed memory-domain design

- `memory-domain-v1.md`: canonical commands, queries, DTOs, evidence, repositories, immutable pack, and idempotency boundaries.
- `memory-provider-v1.md`: provider-neutral projection/search DTOs, lifecycle, bounds, factory separation, and conformance requirements.

This provider contract is internal to Conduit's memory boundary. It is not sent to agents and does not define another handoff envelope. Canonical memory, evidence, context-pack, approval, outbox, and cache types are implemented in their owning memory-domain folders when the corresponding task groups are assigned.
