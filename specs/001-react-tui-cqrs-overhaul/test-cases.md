# QA test cases

## Group 1

- [ ] Documents and `AGENTS.md` describe all five groups, CQRS, security, theme, and review gates without conflicting instructions.

## Group 2

- [ ] Strict TypeScript, lint, Bun tests, Node package build, and standalone build pass.
- [ ] Command/query buses reject duplicate handlers and preserve query immutability.
- [ ] Codex, OpenCode, Pi, and Kilo fixture streams normalize to the same event model; missing executables are visible errors.

## Group 3

- [ ] Bare `conduit` initializes an uninitialized project only after affirmative response.
- [ ] Home search, sidebar selection, lifecycle colors, feature actions, and narrow terminal handling work by keyboard.
- [ ] OS-vault fallback is tested with a fake vault; secrets are absent from project config and logs.

## Group 4

- [ ] Tab/Shift+Tab and Ctrl+Enter operate the form correctly.
- [ ] Reject, quit, resume, discard, approve, and architect-toggle paths preserve the required draft/packet state.
- [ ] Architect activity uses compact structured rows and split diffs without duplicate file counts.

## Group 5

- [ ] Worker monitor shows actual worktree changes, transcript expansion, scrolling, cancellation, and runner errors.
- [ ] Codex review reports findings against approved packet and changed files.
- [ ] Compact CLI paths and interactive refinement route remain backward compatible.
