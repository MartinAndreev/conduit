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

## Approved task group: reviewer verdict and bounded correction workflow

- [x] Treat reviewer process completion separately from review-gate acceptance; only `approved` completes a reviewed run.
- [x] Strengthen reviewer guidance so missing or blocked required verification cannot produce approval.
- [x] Persist structured rejected/needs-changes feedback before scheduling corrections.
- [x] Route path-scoped findings to exactly one writable owning role and fail closed for missing, unowned, or ambiguous paths.
- [x] Reuse verified isolated worktrees for correction turns and integrate correction commits before re-review.
- [x] Run at most two correction rounds, stopping on repeated findings, no observed changes, invalid turns, or limit exhaustion.
- [x] Preserve Feature 007 response validation and the provider contract; use unique assignment IDs for every turn.
- [x] Promote an approved reviewer worktree into an unchanged clean project with a hook-disabled fast-forward-only merge; fail closed and retain worktrees otherwise.
- [x] Resolve assignment context paths from the current role workspace so ACP permission policy keeps parent and sibling worktrees denied.
- [x] Automatically retry structurally or semantically invalid role turns up to two times in the same verified worktree with the exact Conduit validation error.
- [x] Resume failed runs by preserving completed roles, reusing verified failed-role worktrees, and running only unfinished downstream roles with unique retry assignments.
- [x] Persist starting HEAD, feature-package identity, retry counters, and reviewer correction/fingerprint state in run snapshots.
- [x] Share fail-closed resume eligibility between CQRS presentation queries and command revalidation, then atomically claim the failed snapshot.
- [x] Restore disposable reviewer workspaces generically and exempt only the exact Conduit state path during promotion checks.
- [x] Expose failed-run resume as lowercase `[r]` only after eligibility passes, with checking/failure and preserved/retry role presentation, without conflicting with refinement `Ctrl+R`.
- [x] Add integration coverage for approval, correction, routing, rejection, repeat/no-change/limit failures, promotion, truthful completion messages, reviewer-only resume, identity preflight, and atomic resume claims.

## Approved task group: ACP response submission tool

- [x] Inject a Conduit-owned stdio MCP server through ACP session configuration without persistent harness setup.
- [x] Expose a deterministic tool transport for the complete `AgentResponseV1`, including a provider-safe flat verdict mapping, and return bounded validation feedback in the same turn.
- [x] Capture only accepted tool submissions and ignore ACP assistant prose as the authoritative response.
- [x] Remove temporary response-tool state when the native session closes.
- [x] Preserve post-capture Feature 007 structural, semantic, ownership, retry, and recovery behavior.
- [x] Add MCP protocol, prompt-delivery, ACP capture, and malformed-response regression coverage.

## Blockers recorded for later task groups

- Codex app-server v2 remains capability-gated until a captured sanitized fixture verifies its session, event, cancellation, and terminal-response lifecycle; Codex exec JSONL remains the verified fallback.
- Providers remain version-gated to the captured harness versions. Unknown versions may use only a separately verified compatible fallback.
- OpenCode 1.17.18 and Kilo 7.4.9 ACP communicate with Conduit over JSON-RPC stdio. Their process-internal loopback HTTP server is not Conduit's transport and does not change the approved `acp-stdio` boundary.
