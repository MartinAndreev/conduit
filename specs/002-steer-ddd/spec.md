# Steer DDD pilot

Approved scope, 2026-10-02: refactor active-turn correction (Steer) for Codex and OpenCode into human-readable procedures and focused effect steps. This is the first bounded pilot, not an application-wide DDD rewrite.

## Agreed architecture

- `internal/sessions` owns AssignmentRef, its identity types required by the move, and the pure correction rule. `internal/sessions/provider` preserves source compatibility with type aliases. Do not move unrelated IDs, change persistence schema, or duplicate domain DTOs.
- A concrete correction decision contains a typed Kind and explanatory Reason. Callers branch on Kind, not Reason. The current assignment and expected revision must match; identity must remain unchanged; the next revision must be exactly current + 1. Reject overflow rather than wrap to zero.
- Each harness owns `steer.go`, containing its readable procedure and focused private steps. Native input/capability checks, target correlation, delivery, and reconciliation remain adapter-specific.
- Inspect current facts, evaluate the rule, and reserve the pending operation within the same mutex boundary. Release the lock before network/protocol delivery. Do not create a check/reservation race.
- A delivery step has one logical effect. State reservation, acceptance and event publication must be explicit in the flow, not hidden inside network delivery.
- Preserve native acceptance versus semantic application distinction; receipt proves transport acceptance, not that a model applied a correction.
- Preserve Codex late-reply reconciliation and pending state for uncertain delivery. Preserve OpenCode uncertain-delivery handling and post-receipt binding validation. Shared policy does not imply a shared transport skeleton.
- No generic workflow framework, new application orchestration, speculative Skeleton, blanket withRetry, database changes, CLI integration, live model calls, or unrelated fixes.

## Verification

Direct rule tests cover unchanged identity, stale facts/revision, exactly +1, equal/lower/skipped revision, and uint64 overflow. Adapter tests verify rejection before dispatch, concurrency reservation, uncertain delivery, correlated receipts, and unchanged late reconciliation. Test canceled and stale requests where affected by the refactor.

Run formatting, focused race tests, full Go tests, vet, build, and CLI help. Record environment failures separately from code failures; use temporary Go/Turso caches. Baseline ordinary full tests currently fail in Turso linking against the host macOS SDK; harness tests pass. No system configuration changes are authorized by this pilot.

## Handoff

A fresh independent agent uses implementation-audit, with this spec and a bounded before/after snapshot but no implementer conclusions. Its review is advisory. Produce feature-walkthrough for this pilot with a diagram, code reading route, and hotspots; do not describe the entire DDD migration as complete. No commits.

## Human review correction, 2026-10-02

The initial pilot left multi-action failure handling inline in Steer and repeated mutex mechanics. The user requested a correction, not a broader architecture rewrite:

- Extract named delivery-failure subprocedures in both adapters. Keep native rejection, proven unsent failure, and uncertain delivery visibly distinct; preserve error codes, causes, effects and retry semantics.
- Reuse a small shared mutex wrapper within harness infrastructure. Require a synchronous callback; defer unlock, preserve callback errors/panics, and add no retry/recovery behavior. Keep reservation validation and mutation atomic; delivery and publication remain outside the lock.
- Replace repeated direct session lock/unlock mechanics in both steer.go files, including acceptance and late reconciliation. Do not blanket-refactor other session methods or change existing session-handle policy as part of this correction.
- Test wrapper release after callback success/error/panic, plus meaningful failure-path regression checks and focused race tests. Preserve existing Steer checks. This is the first explicit human correction for the pilot; further failed review must follow the correction protocol.

## Approved duplication-audit follow-up, 2026-10-02

The user requested correction of all three duplication-audit findings and explicitly selected unified native-rejection behavior:

- Codex Steer validates request.Session against the receiver session before reservation or delivery. Keep the guard inside the atomic reservation; reuse adapter-local validation where the meaning matches without introducing separately locked checks.
- A Codex result candidate and its event envelope derive their assignment/pending attribution from one consistent snapshot. Preserve correlated native IDs and contract data, event ordering and lock order. Event construction/publication must not hold the session mutex over external work. Use the same emission path rather than duplicated publishers.
- Codex StartTurn and Steer share native failure classification. Explicit native rejection yields delivery_rejected and no automatic retry; preserve the current Steer remote-effect convention for rejection. Uncertain transport failure remains delivery_unknown with reconciliation required. Caller cancellation/deadline retain their specific codes and possibly-sent/reconciliation metadata when applicable. Operation-specific state transitions and late reply correlation stay local.
- Verification includes otherwise-valid wrong-session requests with no dispatch/reservation; deterministic reserve/accept interleaving between snapshot and event publication with matching candidate/envelope attribution; native rejection through both StartTurn and Steer with matching error policy; existing concurrency, late-reply, and uncertainty regressions.
- No unrelated provider, storage, UI, or migration-runner features are added. After verification, the user authorized one commit of the entire current Go rewrite state, including the prior TypeScript removal, Go packages, repository instructions, documentation, and these corrections. Do not push.

## Expanded mutex review and pre-commit audit

The user subsequently requested wrapper adoption in broker.go and other remaining code, and a duplication audit of the entire Go implementation before commit.

- Review all handwritten mutex scopes. Current remaining scopes are confined to harnesses: Codex session/normalization/RPC, OpenCode session/normalization, and the shared event broker. Reuse the existing mandatory-callback mutex wrapper without broadening lock lifetimes or separating atomic checks/mutations.
- Preserve existing lifetime and delivery semantics; do not redesign resource ownership while extracting the mechanism. Report any inherited blocking work inside a lock as an architectural finding rather than silently changing reservation behavior.
- Run integrated race and full checks after both disjoint harness changes settle. Tests remain alongside implementation as confirmed by the user.
- The final duplication audit covers all handwritten Go, including domain/provider ports, storage, process, helpers and the development code generator. Generated repetition is not a handwritten duplication finding. The reviewer must recheck the final frozen harness snapshot. Findings are advisory, not automatic corrections or acceptance decisions.
