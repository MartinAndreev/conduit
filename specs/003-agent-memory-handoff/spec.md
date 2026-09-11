# Feature 003: Agent memory handoff foundation (completed history)

## Status

Completed as a foundation and superseded for future memory work by Feature 007.

This packet originally combined long-term memory, retrieval, context compilation, and agent handoff work. The handoff boundary and the persistence primitives that actually shipped are recorded here. The unimplemented memory design now lives only in `../007-agent-communication-protocol/`.

## Completed outcome

Conduit has the following verified foundations for a future long-term memory layer:

- Feature 002 owns embedded Turso project/global databases, ordered migrations, redaction, configuration resolution, and source-version checksum persistence.
- Feature 007 defines and validates the universal `AgentAssignmentV1`, `AgentResponseV1`, and `ConduitResultRecordV1` contracts. `AgentResponseV1` carries bounded project-memory and global-promotion proposals; agents cannot activate memory or access either database.
- Feature 008 implements validated response capture through harness transports, normalized runtime/result persistence, retry identities, and bounded transcript-free diagnostics, with targeted coverage.
- Feature 009 implements run, assignment, commit, workspace-generation, continuation, and promotion lineage, with targeted coverage.

These are protocol, persistence, and provenance prerequisites. They do not constitute a long-term memory implementation.

A contemporaneous full-suite audit reproduced pre-existing recovery and worktree-continuity failures on the unchanged baseline. This historical record therefore claims implemented primitives and dedicated coverage, not a universally green recovery/worktree baseline.

## Explicitly not implemented by Feature 003

There is no memory domain, memory schema, proposal lifecycle, evidence validator, repository indexer, retrieval provider, Memvid or Hindsight adapter, context-pack compiler, memory cache, projection outbox, deletion workflow, or global-memory promotion implementation in the current source tree.

`source_versions` stores source checksums but is not a repository index. `result_records` stores the latest result read model per run and role but is not immutable memory provenance. The assignment prompt builder references context but does not retrieve or compile memory.

## Successor contract

Feature 007 is the only active design and implementation contract for long-term memory. It preserves the existing V1 wire schemas, defines the provider-neutral memory boundary, and owns all proposed task groups and acceptance criteria. Feature 003 must not be used to justify a separate handoff envelope, vector backlog, or runtime implementation.
