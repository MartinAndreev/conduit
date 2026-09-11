# QA test cases

## Verification status

The feature implementation and focused coverage exist. The September 2026
macOS audit passed typecheck, lint, package build, `pnpm start --help`, and a
macOS ARM64 standalone build, but the full CLI suite reported 354 passing and 21
failing tests. Those failures reproduce on unchanged HEAD: 20 concern later
worktree/correction/resume/continuity behavior and one is a Linux XDG-path
expectation running on macOS. Broad regression checkboxes stay open where this
audit cannot establish the whole historical matrix.

## Group 1

- [x] Documents and `AGENTS.md` describe all five groups, CQRS, security, theme, and review gates without conflicting instructions.

## Group 2

- [ ] Strict TypeScript, lint, Bun tests, Node package build, and standalone build pass.
- [x] Command/query buses reject duplicate handlers and preserve query immutability.
- [x] Codex, OpenCode, Pi, and Kilo fixture streams normalize to the same event model; missing executables are visible errors.

## Group 3

- [x] Bare `conduit` initializes an uninitialized project only after affirmative response.
- [ ] Home search, sidebar selection, lifecycle colors, feature actions, and narrow terminal handling work by keyboard.
- [x] OS-vault fallback is tested with a fake vault; secrets are absent from project config and logs.

## Group 4

- [x] Tab/Shift+Tab and Ctrl+Enter operate the form correctly.
- [x] Reject, quit, resume, discard, approve, and architect-toggle paths preserve the required draft/packet state.
- [x] Architect activity uses compact structured rows and split diffs without duplicate file counts.

## Group 5

- [ ] Worker monitor shows actual worktree changes, transcript expansion, scrolling, cancellation, and runner errors.
- [ ] Codex review reports findings against approved packet and changed files.
- [ ] Compact CLI paths and interactive refinement route remain backward compatible.
