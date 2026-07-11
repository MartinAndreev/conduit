# Contracts

These contracts are the handoff boundary for all task groups. An implementation may not change them without an architect-approved spec update.

- `cqrs.md` defines bus and handler boundaries.
- `feature-provider.md` defines Local Spec Kit and future provider seams.
- `runner-events.md` defines the normalized event stream consumed by React screens.
- `settings-security.md` defines configuration precedence and credential isolation.
