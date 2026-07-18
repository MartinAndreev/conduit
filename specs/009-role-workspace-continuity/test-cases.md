# Feature 009 Test Cases

1. Repository identity is stable for the same Git common directory and differs for unrelated repositories with the same directory basename.
2. Role IDs are encoded into safe non-colliding workspace slot paths.
3. Planning two sequential runs for one repository and role resolves the same physical slot but distinct run/assignment/branch lineage.
4. Two concurrent slot claims produce one winner and one bounded lease conflict naming the owning run.
5. A claim by the same verified run is idempotent.
6. A package, starting HEAD, assignment, registered path, worktree HEAD, or repository mismatch rejects Continue before provider creation.
7. A role that never started may materialize its empty slot during Continue; a previously started missing slot fails closed.
8. Failed, cancelled, interrupted, and promotion-failed runs retain registered slots and Git branches.
9. Reviewer approval plus successful promotion removes all run-owned worktrees, verified temporary branches, leases, and slot records.
10. Start Anew without explicit confirmation performs no mutation.
11. Confirmed Start Anew removes only registered Git worktrees and verified Conduit-owned branches, then plans from current identity.
12. A path or branch identity mismatch blocks destructive cleanup.
13. Role selection displays Continue Existing as the default for compatible retained work and identifies preserved/retry roles.
14. Incompatible retained work displays the bounded reason and permits only Start Anew or Cancel.
15. An active lease displays the owning run and does not offer Continue or destructive reset.
16. TUI components contain no filesystem, Git, process, or database access.
17. Workspace lifecycle behavior is unchanged for arbitrarily named project directories and generated files.
18. Existing run snapshots remain readable; unverifiable legacy worktrees are not silently adopted.
