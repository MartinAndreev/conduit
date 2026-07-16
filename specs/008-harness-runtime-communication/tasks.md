# Feature 008 Tasks

## Approved task group: amendment and baseline communication foundation

- [x] Update Feature 008 documentation to cover session lifecycle, state cleanup, package hashing, legacy migration, clarification-loop correctness, transcripts, and isolation.
- [x] Add versioned normalized telemetry and communication-provider contracts.
- [x] Add a harness-agnostic async-generator stream consumer that awaits persistence in sequence.
- [x] Add provider identifiers and a registry containing distinct preferred and fallback providers for Codex, OpenCode, Pi, and Kilo.
- [x] Add deterministic package hashing service for approved package artifacts.
- [x] Implement concrete native transports using captured sanitized fixtures.
- [x] Add database migrations and repositories for package versions, harness sessions, turns, clarification questions, normalized events, result records, and diagnostic artifacts.
- [x] Replace production run/refinement file repositories after legacy import tests pass.
- [x] Consolidate refinement clarification continuation and repeated-question detection.
- [x] Add transcript retention and workspace isolation enforcement.

## Blockers recorded for later task groups

- Codex app-server v2 remains capability-gated until a captured sanitized fixture verifies its session, event, cancellation, and terminal-response lifecycle; Codex exec JSONL remains the verified fallback.
- Providers remain version-gated to the captured harness versions. Unknown versions may use only a separately verified compatible fallback.
- OpenCode 1.17.18 and Kilo 7.4.9 ACP communicate with Conduit over JSON-RPC stdio. Their process-internal loopback HTTP server is not Conduit's transport and does not change the approved `acp-stdio` boundary.
