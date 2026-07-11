# React TUI and CQRS overhaul

## Outcome

Ship a React/OpenTUI application entered by bare `conduit`, retain scriptable CLI commands, and migrate the application to strict TypeScript with CQRS, explicit provider/credential contracts, and normalized runner events.

## Acceptance criteria

- [ ] Bare `conduit` detects project state, offers initialization when needed, and opens Home for initialized projects.
- [ ] Existing CLI commands retain their names; interactive `refine` opens the React refinement flow while operational commands remain compact by default.
- [ ] The TUI uses shared logo-derived theme tokens and no component hard-codes palette values.
- [ ] Local Spec Kit lists feature packets with explicit metadata-backed lifecycle status and searchable Home sidebar navigation.
- [ ] Refinement drafts are recoverable until preview approval; approval writes the feature packet and optional architect execution begins from the same flow.
- [ ] Codex, OpenCode, Pi, and Kilo have runner adapters with structured event normalization or a clear unavailable-runner state.
- [ ] Credentials are global vault entries selected by project references; no secret is committed or logged.
- [ ] Every task group is reviewed and committed before the next group begins.

## Non-goals

- Implementing GitHub, Linear, Jira, or Asana remote provider adapters.
- Changing feature behavior outside the TUI/CQRS migration.
- Removing compact CLI automation paths.
