# Feature 009 Plan

## Implementation sequence

1. Approve the role-workspace registry, lease, identity, continuation, reset, and cleanup contracts.
2. Add a project-database migration and repository for canonical role workspace slots.
3. Derive collision-resistant repository and role workspace identities and replace run-nested physical paths with registered role slots.
4. Atomically acquire slots before worktree mutation; retain failed slots and release active leases at terminal failure.
5. Delete registered worktrees and verified temporary branches only after successful reviewer promotion.
6. Add a workspace-continuity query and start mode to the run command contract.
7. Add Continue Existing / Start Anew presentation and explicit destructive confirmation to the role selection TUI and CLI.
8. Add narrowly provable legacy adoption or fail-closed reset guidance.

## Verification

- `pnpm --filter conduit-orchestrator test`
- `pnpm --filter conduit-orchestrator typecheck`
- `pnpm --filter conduit-orchestrator lint`
- `pnpm --filter conduit-orchestrator build`
- `pnpm start --help`
