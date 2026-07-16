# Feature 008 Plan

## Implementation sequence

1. Approve amended contracts for the session-oriented communication provider, normalized telemetry, package hashing, session lifecycle, database ownership, legacy migration, transcript retention, and isolation.
2. Add the harness-agnostic async-generator communication interface and stream consumer. Orchestration may depend only on this interface and Feature 007 contracts.
3. Register capability-gated preferred and fallback providers as separate implementations for Codex, OpenCode, Pi, and Kilo. Provider selection happens outside providers and fallback is allowed only before assignment acceptance and side effects.
4. Add deterministic feature-package hashing over approved package artifacts with LF normalization and stable traversal.
5. Add project-database tables for feature package versions, harness sessions, harness turns, clarification questions, normalized runtime events, diagnostic artifacts, and legacy import ledger entries.
6. Move mutable refinement and run state to database repositories. Remove production file-first repositories after idempotent legacy import succeeds.
7. Consolidate refinement continuation so answered clarification questions are persisted before continuation, included in the next turn, and repeated answered questions trigger one automatic reminder before `REPEATED_CLARIFICATION_LOOP`.
8. Add bounded transcript diagnostics and startup/post-run cleanup. Transcripts are never context or read models.
9. Isolate agent workspaces from `.conduit`, state databases, transcripts, temporary launch files, and unrelated worktrees.
10. Remove obsolete compatibility branches after migration and restart tests pass.

## Verification commands

- `pnpm --filter conduit-orchestrator test`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`
- `pnpm --filter conduit-orchestrator build`
- `pnpm start --help`
- Provider fixture suites for Codex app-server, Codex exec JSONL, OpenCode ACP, OpenCode JSON, Pi RPC, Pi JSON, Kilo ACP, and Kilo JSON.
