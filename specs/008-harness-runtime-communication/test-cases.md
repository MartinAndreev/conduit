# Feature 008 Test Cases

The complete acceptance suite is the handoff test matrix A-L. Baseline tests added with the amendment cover:

1. The default provider registry contains eight separate providers: Codex app-server, Codex exec, OpenCode ACP, OpenCode JSON, Pi RPC, Pi JSON, Kilo ACP, and Kilo JSON.
2. Candidate provider selection is external to providers and returns only providers for the requested harness.
3. The communication stream consumer persists events in yielded order and returns the generator terminal result.
4. The consumer awaits each persistence call before reading the next event.
5. Package hashing is stable for identical package content.
6. Package hashing is independent of filesystem traversal order.
7. Package hashing normalizes CRLF/CR line endings to LF.
8. Package hashing changes when approved spec or contract content changes.
9. Package hashing ignores transcripts, run IDs, timestamps, temporary files, and unrelated `.conduit` state.

Later provider fixture tests must include fixture metadata with harness name, harness version, command/protocol, capture date, and sanitization status. Invented native event shapes are not acceptable.
