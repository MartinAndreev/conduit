# Group 2 — TypeScript, React, CQRS, theme, and runner foundation

## Scope

Migrate application source and tests to strict TypeScript/TSX, establish React/OpenTUI bootstrap, the CQRS buses, theme tokens, and typed runner event adapters. Preserve existing command behavior while replacing no user workflow yet.

## Tasks

- [ ] Add React/OpenTUI React, TypeScript, Bun test configuration, and strict compilation to quality checks and CI.
- [ ] Move source into documented TypeScript folders; preserve exported CLI behavior and test coverage.
- [ ] Implement typed command/query buses, registration/bootstrap, handler dependency injection, and unit tests for routing, duplicate registration, handler errors, and immutable query behavior.
- [ ] Implement shared theme tokens from `docs/tui.md` and apply them to a minimal React application shell.
- [ ] Define normalized runner events and adapter interfaces. Implement invocation/availability/event parsing for Codex, OpenCode, Pi, and Kilo using fixtures; absent executables must produce a typed unavailable state.
- [ ] Keep worktree isolation and cancellation semantics intact.

## Acceptance criteria

- [ ] `pnpm lint`, tests, TypeScript check, Node package build, and Bun standalone build pass.
- [ ] Existing CLI smoke tests retain command names, options, and compact output behavior.
- [ ] No TUI screen calls filesystem or child-process APIs directly.
- [ ] Runner adapters parse representative structured streams and reject malformed events safely.
