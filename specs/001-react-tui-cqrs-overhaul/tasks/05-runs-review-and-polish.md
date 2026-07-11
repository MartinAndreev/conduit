# Group 5 — Worker monitoring, review, and polish

## Implementation guardrails

- Run exactly one implementation process and wait for its exit before any continuation.
- Runner event types belong in `src/domains/runs/types`; `src/system/runners` owns only adapter and process-normalization behavior.
- Add persistence behind domain repository interfaces, never direct database access from a screen or helper.
- Use NodeNext ESM `.js` import specifiers, avoid catch-all contract indexes, and do not conceal type failures with `any` or `@ts-expect-error`.
- Verify lint, formatting, typecheck, tests, package build, standalone build, and `pnpm start --help` before handoff.

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
