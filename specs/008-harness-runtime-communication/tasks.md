# Feature 008 Tasks

## Approved task group: amendment and baseline communication foundation

- [x] Update Feature 008 documentation to cover session lifecycle, state cleanup, package hashing, legacy migration, clarification-loop correctness, transcripts, and isolation.
- [x] Add versioned normalized telemetry and communication-provider contracts.
- [x] Add a harness-agnostic async-generator stream consumer that awaits persistence in sequence.
- [x] Add provider identifiers and a registry containing distinct preferred and fallback providers for Codex, OpenCode, Pi, and Kilo.
- [x] Add deterministic package hashing service for approved package artifacts.
- [ ] Implement concrete native transports using captured sanitized fixtures.
- [ ] Add database migrations and repositories for package versions, harness sessions, turns, clarification questions, normalized events, result records, and diagnostic artifacts.
- [ ] Replace production run/refinement file repositories after legacy import tests pass.
- [ ] Consolidate refinement clarification continuation and repeated-question detection.
- [ ] Add transcript retention and workspace isolation enforcement.

## Blockers recorded for later task groups

- Captured sanitized fixtures for Codex app-server v2, OpenCode ACP, Pi RPC, and Kilo ACP are required before implementing preferred providers.
- Preferred providers must remain capability-gated until minimum supported harness versions and protocol compatibility are verified.
