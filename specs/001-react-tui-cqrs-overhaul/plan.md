# Implementation plan

## Status

Historical delivery plan, implemented. Feature 002 later replaced file-backed
runtime persistence, and Features 007–009 strengthened response, communication,
recovery, and workspace-continuity behavior without reopening these task groups.

## Ownership

| Role        | Responsibility                                                          |
| ----------- | ----------------------------------------------------------------------- |
| architect   | contracts, task-group scope, review, commits                            |
| implementer | one approved group through OpenCode using `opencode-go/mimo-v2.5-pro`   |
| reviewer    | Codex review of spec compliance, diff, tests, security, and integration |
| QA          | regression and interaction test coverage                                |

## Historical delivery gate

For every task group, the required sequence was approved spec → OpenCode
implementation → architect review → correction if required → accepted commit.

## Task groups

1. Architecture documents and agent rules.
2. TypeScript, React/OpenTUI, theme, CQRS, and runner-event foundation.
3. Global/project configuration, credential storage, Local Spec Kit provider, metadata, onboarding, and Home.
   3.5. Domain-boundary correction: move existing root application behavior into its owning domains before beginning refinement work.
4. Refinement draft, preview, architect activity, and feature-detail screens.
5. Worker monitoring, review handoff, role portraits, CLI migration completion, and release-quality regression coverage.
