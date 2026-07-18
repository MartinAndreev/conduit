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
11. Separate reviewer turn completion from review-gate acceptance; only an approved verdict completes a reviewed workflow.
12. Persist and path-route rejected or needs-changes findings to exactly one writable owner, then execute at most two correction and re-review rounds in the existing isolated worktrees.
13. Fail closed on unroutable or repeated findings, no-change corrections, invalid turns, unavailable verification, and correction-limit exhaustion.
14. Keep provider and Feature 007 wire contracts unchanged: every correction is a new validated assignment turn, with verified native continuation optional and lineage-linked replacement permitted.
15. After approval, verify project and reviewer Git baselines and fast-forward the approved reviewer worktree into the project checkout; fail closed and retain worktrees when promotion is unsafe.
16. Persist run identity and bounded reviewer/retry state, expose shared resume eligibility through CQRS, atomically claim failed snapshots, and make reviewer cleanup and project cleanliness checks language-independent.
17. Replace ACP final-message JSON delivery with a process-scoped schema tool, validate inside the same native turn, privately capture only accepted `AgentResponseV1`, and preserve existing Feature 007 semantic validation after capture.

## Verification commands

- `pnpm --filter conduit-orchestrator test`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`
- `pnpm --filter conduit-orchestrator build`
- `pnpm start --help`
- Provider fixture suites for Codex app-server, Codex exec JSONL, OpenCode ACP, OpenCode JSON, Pi RPC, Pi JSON, Kilo ACP, and Kilo JSON.
