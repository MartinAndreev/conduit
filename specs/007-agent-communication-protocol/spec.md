# Feature 007: Agent Communication Protocol

## Contract

Conduit replaces Markdown/free-text process handoffs with `AgentAssignmentV1` inputs and one universal `AgentResponseV1` final response for every role. Markdown remains the human-readable format for approved feature artifacts, documentation, rendered research reports, and rendered clarification views.

## Required Separation

1. Runner-native streaming transport: adapter-owned JSON/JSONL/stdout parsing under `src/system/runners`.
2. Normalized Conduit real-time events: `RunnerEvent` records tagged as Conduit-observed, runner-reported, or agent-claimed where applicable.
3. Universal final schema-validated agent response: strict `AgentResponseV1` JSON only.
4. Assignment-specific semantic validation: policies evaluate the same response differently per assignment.
5. Authoritative result metadata: `ConduitResultRecordV1` wraps process, run, role, runner, observed changed files, and validation metadata.
6. Human-readable artifacts: reports and packet Markdown are rendered from validated data and are not the machine protocol.

## Completion Semantics

A role completes successfully only when the process outcome is acceptable, a final `AgentResponseV1` is received, structural validation succeeds, security/path/size validation succeeds, assignment semantic validation succeeds, and `status` is `completed`. Zero exit with missing, malformed, partial, blocked, `needs_input`, or failed protocol output is not successful completion. Non-zero exit with useful partial output is preserved but does not unlock dependents.

## Security

Agents never receive Turso, libSQL, database URL/token, or Conduit database environment variables. Agents may propose project or global memory but cannot activate memory or write any database.
