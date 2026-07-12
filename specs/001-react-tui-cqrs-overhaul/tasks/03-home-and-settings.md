# Group 3 — Settings, Local Spec Kit, onboarding, and Home

## Implementation guardrails

- Run exactly one implementation process. Wait for it to exit and report its result before any continuation or replacement.
- Keep all new contracts in their owning domain; do not recreate global type, interface, or enum indexes.
- Preserve NodeNext ESM `.js` import specifiers and prove the development CLI with `pnpm start --help`.
- Do not hide a boundary failure with `any` or `@ts-expect-error`; add or refine a domain contract instead.

## Scope

Implement platform global settings with local overrides, secure credential storage abstraction, Local Spec Kit feature provider, explicit metadata, initialization/onboarding, and the Home screen.

## Tasks

- [x] Define global configuration location resolver and precedence: CLI option, project config, global config, default.
- [x] Implement credential store with OS-vault primary and encrypted global fallback; project config stores profile identifiers only.
- [x] Implement Local Spec Kit provider and feature metadata read/write contract. Existing packets receive metadata only when first managed and default to `not_started`.
- [x] Make bare `conduit` detect initialized project state; a negative initialization response exits without writes, and acceptance initializes Local Spec Kit without credential collection.
- [x] Implement Home with searchable sidebar, lifecycle dots, selected-feature action modal (View, Refine, Run, Status), welcome/refinement entry, random JSON tip, and role list.
- [x] Add built-in role portrait registry and typed asset-path override configuration; render built-ins with FrameBuffer assets.

## Acceptance criteria

- [x] Local Spec Kit features render in the sidebar with explicit metadata state.
- [x] Search focus and arrow selection are keyboard accessible; narrow terminal state is actionable.
- [x] No credential value appears in config, provider read models, logs, or snapshots.
- [x] Global settings are overridden by project non-secret configuration only.

## Architect review record

Accepted after one explicit OpenCode correction and an architect-owned test-harness fix. Verified with `pnpm lint`, `pnpm format:check`, `pnpm typecheck`, `pnpm test` (68 passing), `pnpm build`, `pnpm build:standalone -- linux-x64`, and `timeout 60 pnpm start --help`.
