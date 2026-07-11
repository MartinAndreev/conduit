# Group 3 — Settings, Local Spec Kit, onboarding, and Home

## Scope

Implement platform global settings with local overrides, secure credential storage abstraction, Local Spec Kit feature provider, explicit metadata, initialization/onboarding, and the Home screen.

## Tasks

- [ ] Define global configuration location resolver and precedence: CLI option, project config, global config, default.
- [ ] Implement credential store with OS-vault primary and encrypted global fallback; project config stores profile identifiers only.
- [ ] Implement Local Spec Kit provider and feature metadata read/write contract. Existing packets receive metadata only when first managed and default to `not_started`.
- [ ] Make bare `conduit` detect initialized project state; a negative initialization response exits without writes, and acceptance initializes Local Spec Kit without credential collection.
- [ ] Implement Home with searchable sidebar, lifecycle dots, selected-feature action modal (View, Refine, Run, Status), welcome/refinement entry, random JSON tip, and role list.
- [ ] Add built-in role portrait registry and typed asset-path override configuration; render built-ins with FrameBuffer assets.

## Acceptance criteria

- [ ] Local Spec Kit features render in the sidebar with explicit metadata state.
- [ ] Search focus and arrow selection are keyboard accessible; narrow terminal state is actionable.
- [ ] No credential value appears in config, provider read models, logs, or snapshots.
- [ ] Global settings are overridden by project non-secret configuration only.
