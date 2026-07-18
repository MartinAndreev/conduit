# Role Workspace V1 Contract

`RoleWorkspaceRepository` is the canonical project-scoped persistence boundary for physical role workspace slots. This version defines persistence and fenced lease primitives; Git materialization is a later task group.

## Current slot

A slot contains:

- `repositoryId`
- normalized `roleKey`
- positive `generation`
- canonical `workspacePath`
- `owningRunId`
- `state`: `provisioning | ready | running | retained | promotion-pending | promoted | cleanup-pending | quarantined`
- `startingHead`
- `packageHash`
- `assignmentHash`
- optional `worktreeHead`
- exact `branchName`
- optional `leaseOwner`
- non-negative `fencingToken`
- optional `leasedAt`
- `createdAt`
- `updatedAt`

Primary identity is `(repositoryId, roleKey)`. Workspace paths and branch names are unique among current slots.

## Generation lineage

Every generation is also written to append-only lineage keyed by `(repositoryId, roleKey, generation)`. It preserves owning run, starting HEAD, package/assignment hashes, branch ref, last verified branch OID, outcome, promotion OID, and timestamps. Advancing a slot MUST NOT rewrite an older generation.

## Operations

- `load(repositoryId, roleKey)` returns the canonical current slot.
- `claim(input)` atomically creates a slot or claims a compatible unleased slot. It returns `claimed`, `lease-conflict`, or `identity-conflict` with bounded owner metadata.
- `claimAll(inputs)` claims the complete role set in one database transaction and rolls back every claim when any role conflicts.
- Repeating `claim` with the same run and lease owner is idempotent and returns the existing fencing token.
- A new successful lease after release increments the fencing token. Provider launch is forbidden until materialization has an observed canonical HEAD, that HEAD is fenced into the slot, and the run snapshot callback has completed. A provisioning slot without a recorded HEAD is reconstructable only when its exact registered path, common directory, branch, clean status, and observed HEAD can be proven, or when both its path and branch are proven absent.
- `retain(identity, worktreeHead)` changes an owned slot to retained and clears the lease only when run, owner, and fencing token match.
- `transition(identity, expectedState, nextState)` performs a fenced compare-and-set.
- `recordHead`, `completeGeneration`, and `remove` require the current fenced identity; current-slot removal never deletes generation lineage. Repeating `completeGeneration` with byte-identical OIDs and outcome is successful; conflicting completion is rejected.
- `listCleanupPending(repositoryId)` supports startup/run-boundary reconciliation of exact registered paths and refs after cleanup crashes.
- `advanceAll(previous, next)` atomically advances every selected abandoned generation after Git reconciliation, writes new append-only lineage, assigns the new run and lease, and increments fencing tokens.
- Start Anew preserves the previous branch and exact OID, removes only the registered worktree checkout, and may resume safely after a crash before the atomic generation advance.

## Lease policy

The initial implementation keeps Conduit's exclusive project-database process lock. It does not expire or steal a lease based on elapsed time. Every mutating repository operation requires the current lease owner and fencing token. Lease conflict never overwrites another owner.

## Identity

Repository identity is a SHA-256 digest of the real absolute Git common-directory path returned by Git. Checkout basename, remote URL, and user-provided labels are not identity.

Role keys are Unicode NFC normalized, case-folded to lowercase, and restricted to ASCII letters, digits, dot, underscore, and hyphen. Empty keys, separators, traversal components, control characters, and distinct configured names that normalize to one key are rejected.
