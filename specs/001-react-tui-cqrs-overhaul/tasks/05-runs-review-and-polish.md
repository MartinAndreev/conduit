# Group 5 — Worker monitoring, review, and polish

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
