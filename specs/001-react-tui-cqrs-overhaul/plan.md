# Implementation plan

## Ownership

| Role        | Responsibility                                                          |
| ----------- | ----------------------------------------------------------------------- |
| architect   | contracts, task-group scope, review, commits                            |
| implementer | one approved group through OpenCode using `opencode-go/mimo-v2.5-pro`   |
| reviewer    | Codex review of spec compliance, diff, tests, security, and integration |
| QA          | regression and interaction test coverage                                |

## Delivery gate

For every task group: approved spec → OpenCode implementation → architect review → correction if required → accepted commit. The implementer must not start a later group.

## Task groups

1. Architecture documents and agent rules.
2. TypeScript, React/OpenTUI, theme, CQRS, and runner-event foundation.
3. Global/project configuration, credential storage, Local Spec Kit provider, metadata, onboarding, and Home.
4. Refinement draft, preview, architect activity, and feature-detail screens.
5. Worker monitoring, review handoff, role portraits, CLI migration completion, and release-quality regression coverage.
6. SQLite state persistence, migration from local JSON run state, and data-recovery tooling.

## Deferred persistence direction

SQLite is the planned state store after the React workflow is stable. It must not replace committed Markdown specifications: feature packets remain file-based and reviewable in Git.

- Global state lives in the platform data directory (for example, `~/.local/share/conduit/conduit.db`) and holds non-secret preferences, recent-project metadata, and references to credential profiles.
- Project-local mutable state lives in ignored `.conduit/state.db` and holds drafts, feature lifecycle state, runs, normalized runner events, transcript indexes, changed-file snapshots, and review results.
- Use WAL mode, schema migrations, and append-only runner-event records so the TUI can recover and replay interrupted runs.
- Credentials remain in the operating-system keychain where available. SQLite may hold a credential reference or encrypted fallback ciphertext, never plaintext secrets.
- Prefer Kysely as the typed query layer with explicit SQL migrations. Before adoption, complete a small compatibility spike for the Node package and Bun standalone binary; select the SQLite driver from that evidence rather than assuming one runtime.
