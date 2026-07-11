# Conduit architecture

## Layers

Conduit is a TypeScript CLI with a React/OpenTUI presentation layer.

| Layer                                      | Responsibility                                                                      | May depend on                        |
| ------------------------------------------ | ----------------------------------------------------------------------------------- | ------------------------------------ |
| `src/tui`                                  | Screens, sections, components, view hooks, keyboard routing                         | application read models and commands |
| `src/commands`, `src/queries`              | Application use cases and handlers                                                  | system services, contracts, helpers  |
| `src/system`                               | Bootstrap, buses, configuration, providers, credentials, runners, process lifecycle | contracts, helpers                   |
| `src/types`, `src/enums`, `src/interfaces` | Shared application contracts                                                        | no implementation layer              |
| `src/helpers`                              | Pure formatting, string, file, and parsing helpers                                  | platform libraries only              |

## CQRS

Commands are explicit intent objects dispatched through `CommandBus`; exactly one handler performs each mutation or side effect. Queries are explicit read requests dispatched through `QueryBus`; handlers return immutable read models and do not mutate state.

Screens dispatch commands and execute queries through controller hooks. They must not call filesystem, child-process, provider, or credential APIs directly.

## Feature lifecycle

Every approved feature packet has committed metadata with one lifecycle value: `not_started`, `in_progress`, or `implemented`. Drafts are ignored local runtime state and do not create a committed feature packet until preview approval.

## Runner events

Runner-specific output is normalized before the UI receives it. The normalized event model includes lifecycle, thought/activity, tool invocation, tool output, file patch, error, and final result events. Codex, OpenCode, Pi, and Kilo adapter differences remain in `src/system/runners`.
