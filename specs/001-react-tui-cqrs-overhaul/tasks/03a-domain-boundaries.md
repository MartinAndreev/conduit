# Group 3.5 — Domain-boundary correction

## Scope

Correct the partial domain migration before Group 4 begins. This group is structural: preserve public CLI behavior while relocating application behavior to its owning domains.

## Required layout

Each domain owns its own `commands`, `queries`, `handlers`, `repositories`, `types`, `interfaces`, `enums`, and `errors` boundaries. A folder is created only when that category has a real domain artifact; do not add empty placeholder files. Domain modules may expose a narrow barrel at the domain boundary.

Interfaces, enums, and shared value types must not be declared in implementation files. Command and query message contracts belong in `interfaces/commands` and `interfaces/queries`; repository contracts belong in `interfaces`; value types belong in `types`; enums belong in `enums`; errors belong in `errors`. `commands`, `queries`, `handlers`, and `repositories` contain executable behavior only.

`src/system` contains only cross-cutting infrastructure (bootstrap, buses, runner transport, database driver/migrations). `src/tui` contains only presentation, view controllers, and UI assets. Neither owns domain rules or persistence access.

## Tasks

- [ ] Move root configuration behavior from `src/config.ts` into `src/domains/configuration` repositories/handlers/types.
- [ ] Move root feature-packet behavior from `src/features.ts` into `src/domains/features` repositories/handlers/types.
- [ ] Move root run planning/execution behavior from `src/runs.ts` into `src/domains/runs` commands/handlers/repositories/types.
- [ ] Move root skill and role behavior from `src/skills.ts` and role-template handling into `src/domains/roles`.
- [ ] Move legacy `src/commands/*` handlers into their owning domain command and handler folders; preserve Commander as a thin CLI adapter only.
- [ ] Move every command/query interface and result/read-model interface out of `commands`, `queries`, handlers, and repositories into the owning domain interface/type folder.
- [ ] Remove root application modules once imports are migrated. No compatibility re-export files may remain at the root.
- [ ] Update bootstrap composition so all handlers receive only their domain dependencies/repositories.
- [ ] Add import-boundary tests that fail if application behavior is reintroduced at `src/` root or if a TUI module reads files/spawns processes directly.

## Acceptance criteria

- [ ] No root `src/config.ts`, `src/features.ts`, `src/runs.ts`, `src/skills.ts`, or `src/commands/` implementation remains.
- [ ] `src/system` has no product-domain rules and `src/tui` has no filesystem, process, credential, provider, or repository access.
- [ ] Each handler receives explicit domain dependencies; no global service locator is introduced.
- [ ] CLI command names, options, compact behavior, lint, formatting, strict typecheck, tests, package build, standalone build, and `pnpm start --help` all pass.
