# Feature 009: Role Workspace Continuity

## Status

Approved for the initial task group defined in `tasks.md`.

## Problem

Conduit currently materializes role worktrees below a run-specific path. Failed worktrees are retained for recovery, but repeated runs accumulate nested paths and operators must reason about runs before discovering reusable role work. Simply changing the path to a role name would be unsafe: a filesystem path is not workflow identity and can contain work from a different revision, package, assignment, or process.

## Goal

Conduit MUST manage one physical workspace slot per repository identity and role while preserving run-scoped identity, results, events, and lineage in `state.db`. Failed or interrupted compatible work remains resumable. A successfully approved and promoted workflow removes its role slots and temporary branches. Starting anew is an explicit destructive operation.

## Non-goals

- Sharing one role slot between concurrent runs.
- Continuing work based only on directory existence or role name.
- Reusing provider-native chat state without Feature 008 identity verification.
- Replacing run IDs, assignment IDs, result records, or event identity with role IDs.
- Adding language-, framework-, package-manager-, or generated-directory-specific cleanup rules.
- Adopting legacy worktrees whose repository, package, assignment, or Git lineage cannot be proven.

## Terminology

- **Repository identity**: a stable Conduit identifier derived from the repository Git common directory identity, never from the checkout basename alone.
- **Role workspace slot**: the single physical Git worktree location assigned to one role within one repository identity.
- **Lease**: an atomic database claim granting one run exclusive mutation rights to a role slot.
- **Continue**: resume a retained run after verifying its complete identity envelope.
- **Start anew**: explicitly discard eligible retained role slots and their temporary branches before planning a new run.

## Required behavior

### Workspace addressing

Physical paths MUST be derived from repository identity and normalized role identity:

```text
<worktree-root>/<repository-id>/<role-id>
```

The path MUST NOT include a run ID. Run ID remains part of every database record, event, assignment, result, checkpoint, and branch lineage.

Role identifiers MUST be encoded safely. Two distinct repository identities or role identifiers MUST NOT collide.

### Canonical registry

Project `state.db` MUST contain the canonical role-workspace registry plus append-only workspace-generation lineage. Each slot records at least:

- repository identity;
- normalized role key, generation, and workspace path;
- owning run ID;
- slot state;
- starting project HEAD;
- feature-package hash;
- assignment identity hash;
- current worktree HEAD;
- temporary branch name;
- lease owner, fencing token, and lease timestamp;
- creation and update timestamps.

The filesystem and branch list are observations, not authoritative workflow state.

### Exclusive lease

A run MUST atomically claim every role slot it will mutate before launching agents. A slot may be claimed only when unowned, already owned by the same verified run and lease owner, or explicitly advanced through Start Anew. Two processes MUST NOT launch the same role concurrently.

Each successful new claim receives a monotonically increasing fencing token. Every later state transition compares repository ID, role key, run ID, lease owner, and fencing token. An older owner cannot mutate the slot after a successor claim.

The initial implementation retains the existing exclusive project-database process lock and does not steal leases by TTL. A conflicting claim fails before workspace mutation and reports the owning run. Crash recovery may release a lease only through a later reconciler that proves the database process owner is dead and verifies the registered Git state; time passage alone never authorizes deletion or takeover.

### Continue

Continue uses Feature 008 resume semantics. Conduit MUST verify:

- repository identity;
- original project HEAD;
- feature-package hash including ownership inputs;
- role and assignment identity;
- persisted result and checkpoint lineage;
- registered workspace path and observed Git top-level;
- registered and observed worktree HEAD;
- lease availability.

Compatible completed roles remain preserved. Failed roles and unfinished downstream roles alone are scheduled. A missing workspace for a role that never started may be created in its slot. A missing or divergent workspace that previously started fails closed.

### Start anew

Start Anew is destructive and MUST require an explicit command field and TUI confirmation. It MUST show the affected retained run and roles and default to Cancel. Before reusing the physical slot, Conduit preserves the previous generation's branch and exact OID as append-only lineage. It refuses Start Anew when tracked, untracked, ignored, or in-progress Git state cannot be represented by the verified checkpoint commit. After acquiring an exclusive fenced transition, Conduit removes the registered worktree with Git-aware removal, retains the prior verified branch/OID, advances the slot generation, and plans the new run from the current approved package and project HEAD.

Start Anew never silently deletes an unpromoted branch or recoverable commit. Conduit MUST never recursively delete an arbitrary path based only on its name.

### Start-run presentation

Before dispatching a new run, the role-selection flow queries workspace continuity for the selected feature and roles. It displays one of:

- no retained work: Start New;
- compatible retained work: Continue Existing (default) or Start Anew;
- incompatible retained work: reason plus Start Anew or Cancel;
- active lease conflict: owning run and Cancel.

Views render the read model and dispatch commands. They do not inspect Git, databases, processes, or files.

### Completion and failure

After reviewer approval and successful fast-forward promotion, Conduit MUST remove every role workspace slot owned by that run, delete verified temporary branches, release leases, and remove slot records before reporting cleanup complete. Structured run history remains in `state.db`.

Failed, cancelled, interrupted, or promotion-failed workflows retain their slots and release active process leases into a retained state. Cleanup retention MUST NOT silently delete a resumable slot. An explicit Start Anew or separately approved retention policy may remove it.

### Generic cleanup

Role-slot cleanup uses Git worktree removal, exact registered paths, verified branch ownership, and full disposable reset/clean behavior. It MUST NOT select paths named `node_modules`, `vendor`, `dist`, `build`, or any language/tool-specific directory.

## Compatibility

- Existing run-scoped worktrees remain readable migration inputs.
- A legacy slot may be adopted only when its run snapshot and Git identity prove the complete required envelope; otherwise it remains non-continuable and requires explicit Start Anew.
- Feature 007 wire contracts and Feature 008 retry, correction, promotion, and resume semantics remain unchanged.
- Existing stored run history remains readable.

## Acceptance criteria

1. Two repositories with the same checkout basename produce different workspace namespaces.
2. A repository has at most one physical slot per normalized role.
3. Concurrent claims for one role permit exactly one run.
4. A crash leaves failed role work available and a compatible Continue schedules only failed/unfinished roles.
5. Package, HEAD, assignment, repository, workspace, or lease mismatch fails before agent launch.
6. Start Anew requires explicit confirmation and removes only registered Conduit worktrees and branches.
7. Approved promoted workflows remove all owned role slots and temporary branches.
8. Failed, cancelled, interrupted, and promotion-failed workflows retain their slots.
9. TUI and CLI use the same CQRS continuity query and start/continue/reset commands.
10. No lifecycle behavior depends on project language or conventional dependency/generated directory names.
