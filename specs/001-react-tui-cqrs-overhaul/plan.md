# Implementation plan

## Ownership

| Role        | Responsibility                                                          |
| ----------- | ----------------------------------------------------------------------- |
| architect   | contracts, task-group scope, review, commits                            |
| implementer | one approved group through OpenCode using `opencode-go/mimo-v2.5-pro`   |
| reviewer    | Codex review of spec compliance, diff, tests, security, and integration |
| QA          | regression and interaction test coverage                                |

## Delivery gate

For every task group: approved spec → OpenCode implementation → architect review → correction if required → accepted commit. The implementer must not start a later group.

## Task groups

1. Architecture documents and agent rules.
2. TypeScript, React/OpenTUI, theme, CQRS, and runner-event foundation.
3. Global/project configuration, credential storage, Local Spec Kit provider, metadata, onboarding, and Home.
4. Refinement draft, preview, architect activity, and feature-detail screens.
5. Worker monitoring, review handoff, role portraits, CLI migration completion, and release-quality regression coverage.
