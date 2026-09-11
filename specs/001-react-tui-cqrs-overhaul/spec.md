# React TUI and CQRS overhaul

## Delivery status

Implemented across the v0.3.0 overhaul and later corrective work. React/OpenTUI,
CQRS, provider and credential boundaries, refinement, worker monitoring, review,
shared hooks, and `SplitDiff` are present. Later persistence and runtime features
superseded some original implementation details. The current full macOS CLI
suite is not green, so this status does not claim that every historical QA gate
passes today.

## Outcome

Ship a React/OpenTUI application entered by bare `conduit`, retain scriptable CLI commands, and migrate the application to strict TypeScript with CQRS, explicit provider/credential contracts, and normalized runner events.

## Acceptance criteria

- [x] Bare `conduit` detects project state, offers initialization when needed, and opens Home for initialized projects.
- [x] Existing CLI commands retain their names; interactive `refine` opens the React refinement flow while operational commands remain compact by default.
- [x] The TUI uses shared logo-derived theme tokens and no component hard-codes palette values.
- [x] Local Spec Kit lists feature packets with explicit metadata-backed lifecycle status and searchable Home sidebar navigation.
- [x] Refinement drafts are recoverable until preview approval; approval writes the feature packet and optional architect execution begins from the same flow.
- [x] Codex, OpenCode, Pi, and Kilo have runner adapters with structured event normalization or a clear unavailable-runner state.
- [x] Credentials are global vault entries selected by project references; no secret is committed or logged.
- [x] Every task group was reviewed and committed through the historical delivery sequence.

## Non-goals

- Implementing GitHub, Linear, Jira, or Asana remote provider adapters.
- Changing feature behavior outside the TUI/CQRS migration.
- Removing compact CLI automation paths.
