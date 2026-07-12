# Group 5 — Worker monitoring, review, and polish

## Implementation guardrails

- Run exactly one implementation process and wait for its exit before any continuation.
- Runner event types belong in `src/domains/runs/types`; `src/system/runners` owns only adapter and process-normalization behavior.
- Add persistence behind domain repository interfaces, never direct database access from a screen or helper.
- Use NodeNext ESM `.js` import specifiers, avoid catch-all contract indexes, and do not conceal type failures with `any` or `@ts-expect-error`.
- Verify lint, formatting, typecheck, tests, package build, standalone build, and `pnpm start --help` before handoff.

## Architect implementation notes

- Read this task, the active packet, `AGENTS.md`, and the existing runs, runner-adapter, and TUI code before editing. This file is the implementation source of truth.
- Build the worker monitor through runs-domain commands, queries, handlers, repository interfaces, and normalized `RunnerEvent` values. TUI screens, sections, components, and controllers must not read files, spawn runners, parse diffs, or contain business rules.
- The changed-file view must derive from the role worktree's actual `git diff`; captured agent output may supplement activity but can never be treated as the authoritative patch.
- Keep runner normalization in `src/system/runners` and runner event contracts in `src/domains/runs/types`. Missing executables produce an unavailable lifecycle/error event, not a process exception reaching presentation.
- The review handoff needs an explicit runs-domain command/query and persisted result containing decision, findings, evidence paths, and required follow-up. It reads the approved packet, selected run, authoritative diffs, test output, and unresolved risks; it must not be initiated from a TUI component.
- Preserve compact CLI behavior and the completed refinement revision loop. Do not begin Group 6, add remote providers, or revive discarded pixel/image experiments.
- Keep every new cross-domain import on the established `@domains`, `@system`, `@tui`, or `@helpers` aliases. Use strict TypeScript; no `any`, `@ts-expect-error`, catch-all contract indexes, or `.ts` runtime specifiers.
- Add behavioral tests for unavailable runners, normalized event flows, cancellation, diff precedence, persisted review results, and CLI compatibility. Prefer focused tests over shallow snapshot assertions.

## Scope

Finish the React run experience, preserve compact CLI operations, add architect review handoff, and complete role portraits and regression coverage.

## Tasks

- [ ] Implement React worker monitor driven by normalized events for all runner adapters.
- [ ] Show role lifecycle, activity timeline, transcript expansion, worktree file changes, split diff, scrolling, cancellation, and unavailable-runner guidance.
- [ ] Preserve compact `init`, `feature`, `run`, and `status` operations; interactive `refine` uses the shared screen.
- [ ] Add a Codex review command/query flow that reads the approved packet, selected run, changed files, test output, and unresolved risks.
- [ ] Finalize built-in nerdy FrameBuffer robot portraits and config overrides for all default roles.
- [ ] Complete release documentation, migration notes, accessibility/resize testing, and end-to-end regression tests.

## Acceptance criteria

- [ ] Actual worktree diffs, not agent claims, drive the changed-file view.
- [ ] A review result identifies approved/rejected status, findings, evidence paths, and required follow-up.
- [ ] All CLI, runner, provider, security, and TUI regression tests pass.
