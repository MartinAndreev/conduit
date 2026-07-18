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
10. A completed reviewer turn with an approved verdict completes the reviewed workflow without a correction turn.
11. Completed reviewer turns with `needs_changes` or `rejected` route each path-scoped finding only to its unique writable owner, persist feedback, integrate observed corrections, and re-review.
12. Missing, pathless, unowned, or ambiguously owned required findings fail closed without broadcasting correction work.
13. An identical repeated review, a correction with no observed file changes, an invalid correction response, or two exhausted correction rounds fails the workflow.
14. Reviewer process completion alone never emits a successful reviewed-flow message; final success requires an approved verdict.
15. Every correction and re-review uses a unique assignment ID and retains strict Feature 007 validation.
16. Reviewer verification removes known generated output and does not leave source or unexpected changes in the read-only review worktree.
17. An approved reviewer worktree is fast-forwarded into the project checkout only when project HEAD and cleanliness remain verified; conflicts, divergence, or local material changes fail the run and retain recovery worktrees.
18. ACP assignments explicitly resolve context references inside the current role workspace; in-workspace packet reads are allowed while parent and sibling paths remain denied.
19. A missing, structurally invalid, or semantically invalid role response automatically retries at most twice in the same verified worktree, receives the exact Conduit validation error, preserves checkpointed files, and does not stop independent parallel roles; valid non-completed responses do not protocol-retry.
20. Resuming a failed run after process restart verifies persisted starting HEAD, package hash, results, commits, and worktree baselines; preserves completed roles; retries failed roles; and runs only unfinished downstream roles with unique retry assignment IDs.
21. Failed-run dashboards show checking, resumable, or non-resumable recovery state and preserved/retry roles. They show `[r] Resume failed run`, never `Ctrl+R`, only after eligibility passes.
22. Two concurrent resume commands can atomically claim a failed snapshot only once.
23. Reviewer cleanup resets and cleans the complete disposable review worktree without language-, dependency-, build-, or cache-directory allowlists; project promotion exempts only the exact Conduit state path.
24. ACP session creation supplies one process-scoped Conduit MCP server without modifying global or project harness configuration.
25. The response tool rejects a malformed nested `AgentResponseV1` with bounded field feedback and accepts a corrected call in the same server session.
26. ACP assistant prose, including JSON-looking prose, is ignored while the response tool is active; only an accepted private capture becomes the terminal response candidate.
27. Response-tool temporary files are unavailable after communication-session cleanup.

Later provider fixture tests must include fixture metadata with harness name, harness version, command/protocol, capture date, and sanitization status. Invented native event shapes are not acceptable.
