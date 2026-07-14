# Conduit architecture

## Domain-first layout

Conduit is a TypeScript CLI with a React/OpenTUI presentation layer. Application code is organized by domain rather than by one global category of types or handlers.

```text
src/
  domains/
    runs/
      commands/  queries/  handlers/  repositories/
      types/     interfaces/  enums/  errors/
    refinement/
    features/
    configuration/
    roles/
  tui/
    components/  sections/  screens/
  system/
    bootstrap/  bus/  storage/  runners/
  helpers/
    file/  formatting/  string/
```

Each domain owns its commands, queries, handlers, types, interfaces, enums, errors, and repositories. Do not create catch-all `types`, `enums`, or `interfaces` modules at the application root. A small domain barrel is acceptable when it makes an intentional public boundary clearer.

## Repository and database boundary

Only repository implementations access embedded Turso through Kysely. A repository receives a database connection through its constructor and exposes a domain-specific interface; command and query handlers receive repository instances through application dependency injection. Screens, components, helpers, and domain types must never access the database directly. See the [database API guide](../packages/conduit/docs/database-api.md) for the complete storage contract.

`src/system/bootstrap/application.ts` is the small composition root. Repository and lifecycle construction is isolated in `createBootstrapComposition`; logical `ApplicationBootstrapService` implementations register core, configuration, feature, role, refinement, and run handlers on the command/query buses. New domains extend the bootstrap contract instead of adding registrations directly to `application.ts`.

| Layer         | Responsibility                                                                        | May depend on                        |
| ------------- | ------------------------------------------------------------------------------------- | ------------------------------------ |
| `src/tui`     | Screens, sections, components, view hooks, keyboard routing                           | application read models and commands |
| `src/domains` | Domain use cases, contracts, errors, and repository interfaces/implementations        | system contracts, helpers            |
| `src/system`  | Bootstrap, buses, database driver, providers, credentials, runners, process lifecycle | domains, helpers                     |
| `src/helpers` | Pure formatting, string, file, and parsing helpers                                    | platform libraries only              |

## CQRS

Commands are explicit intent objects dispatched through `CommandBus`; exactly one handler performs each mutation or side effect. Queries are explicit read requests dispatched through `QueryBus`; handlers return immutable read models and do not mutate state.

Screens dispatch commands and execute queries through controller hooks. They must not call filesystem, child-process, provider, or credential APIs directly.

## Feature lifecycle

Every approved feature packet has committed metadata with one lifecycle value: `not_started`, `in_progress`, or `implemented`. Drafts are ignored local runtime state and do not create a committed feature packet until preview approval.

## Runner events

Runner-specific output is normalized before the UI receives it. The normalized event model includes lifecycle, thought/activity, tool invocation, tool output, file patch, error, and final result events. Codex, OpenCode, Pi, and Kilo adapter differences remain in `src/system/runners`.
