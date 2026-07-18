# Feature 009 Database Contract

Project `state.db` adds two tables.

## `role_workspace_slots`

| Column            | Constraint             |
| ----------------- | ---------------------- |
| `repository_id`   | primary-key component  |
| `role_key`        | primary-key component  |
| `generation`      | positive, not null     |
| `workspace_path`  | not null, unique       |
| `owning_run_id`   | not null               |
| `state`           | not null               |
| `starting_head`   | not null               |
| `package_hash`    | not null               |
| `assignment_hash` | not null               |
| `worktree_head`   | nullable               |
| `branch_name`     | not null, unique       |
| `lease_owner`     | nullable               |
| `fencing_token`   | non-negative, not null |
| `leased_at`       | nullable               |
| `created_at`      | not null               |
| `updated_at`      | not null               |

Primary key: `(repository_id, role_key)`.

## `role_workspace_generations`

Append-only lineage keyed by `(repository_id, role_key, generation)` with owning run, identity hashes, workspace path, branch name, branch OID, outcome, promotion OID, `created_at`, and optional `completed_at`.

Claims and state transitions use conditional database updates. The initial implementation does not steal leases by timestamp. Files under the worktree root and legacy worktree metadata JSON are not canonical slot state.
