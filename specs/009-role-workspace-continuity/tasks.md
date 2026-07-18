# Feature 009 Tasks

## Approved task group: slot identity, persistence, and fenced lease primitives

- [x] Add versioned role-workspace slot, generation-lineage, and lease contracts.
- [x] Add canonical project-database tables for current slots and append-only generations.
- [x] Derive stable repository identity from the canonical Git common directory.
- [x] Normalize and validate role keys against traversal, separator, Unicode, and case-folding collisions.
- [x] Add atomic claim, idempotent same-owner claim, retain, and fenced compare-and-set repository operations.
- [x] Preserve the existing exclusive project-database process lock; do not implement TTL lease stealing.
- [x] Add migration, repository, identity, collision, contention, fencing, and lineage tests.

This group does not change current worktree paths, cleanup, promotion, start commands, or TUI behavior.

## Approved task group: role-slot materialization and terminal lifecycle

- [x] Replace run-nested worktree paths with registered repository/role slots while retaining run-scoped assignment and branch lineage.
- [x] Acquire fenced role claims before Git mutation and fail closed on conflicts.
- [x] Reconcile provisioning intent with observed Git worktree/common-dir/branch/HEAD state after crashes.
- [x] Retain failed/interrupted/cancelled slots and disable time-based deletion of resumable work.
- [x] Record successful promotion before removing registered worktrees and verified promoted branches.
- [x] Leave cleanup failures in `cleanup-pending` without reversing promotion.
- [x] Add integration coverage for crash reuse, conflict-before-launch, retention, promotion, lineage anchoring, and cleanup.

## Approved task group: continuation and start-mode presentation

- [x] Add a workspace-continuity query for selected feature and roles.
- [x] Extend start-run commands with explicit `continue` and `start-new` modes.
- [x] Reuse Feature 008 eligibility for Continue.
- [x] Require explicit destructive confirmation for Start Anew, preserve prior branch/OID lineage, and reject uncheckpointed data.
- [x] Render Continue Existing, Start Anew, incompatibility, and lease-conflict states in the TUI without filesystem access.
- [x] Route CLI and TUI through shared CQRS continuity/resume boundaries and the same start-mode contract.
- [x] Add presentation, service, repository, CLI, and continuity coverage.

## Deferred blockers

- Automatic adoption of legacy worktrees is deferred unless the complete identity envelope can be proven.
- Parallel active runs that require the same role are intentionally unsupported.
- Lease TTL/heartbeat stealing is deferred while the exclusive project database lock remains authoritative.
