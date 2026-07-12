# Group 2 — TypeScript, React, CQRS, theme, and runner foundation

## Scope

Migrate application source and tests to strict TypeScript/TSX, establish React/OpenTUI bootstrap, the CQRS buses, theme tokens, and typed runner event adapters. Preserve existing command behavior while replacing no user workflow yet.

## Tasks

- [x] Add React/OpenTUI React, TypeScript, Bun test configuration, and strict compilation to quality checks and CI.
- [x] Move source into documented TypeScript folders; preserve exported CLI behavior and test coverage.
- [x] Implement typed command/query buses, registration/bootstrap, handler dependency injection, and unit tests for routing, duplicate registration, handler errors, and immutable query behavior.
- [x] Implement shared theme tokens from `docs/tui.md` and apply them to a minimal React application shell.
- [x] Define normalized runner events and adapter interfaces. Implement invocation/availability/event parsing for Codex, OpenCode, Pi, and Kilo using fixtures; absent executables must produce a typed unavailable state.
- [x] Keep worktree isolation and cancellation semantics intact.

## Acceptance criteria

- [x] `pnpm lint`, tests, TypeScript check, Node package build, and Bun standalone build pass.
- [x] Existing CLI smoke tests retain command names, options, and compact output behavior.
- [x] No TUI screen calls filesystem or child-process APIs directly.
- [x] Runner adapters parse representative structured streams and reject malformed events safely.

## Architect review record

Accepted after direct review and domain-layout correction. Verified with `pnpm lint`, `pnpm format:check`, `pnpm typecheck`, `pnpm test` (54 passing), `pnpm build`, `pnpm build:standalone -- linux-x64`, and `pnpm start --help`.
