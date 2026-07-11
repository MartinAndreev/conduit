# Group 4 — Refinement and architect activity

## Scope

Implement the shared refinement route used by Home and interactive `conduit refine`, including drafts, preview approval, and structured architect activity.

## Tasks

- [ ] Implement ignored recoverable draft storage with Resume and Discard query/commands.
- [ ] Implement the multi-field tabbed refinement form with field guidance, Tab/Shift+Tab navigation, and Ctrl+Enter submit.
- [ ] Render Markdown preview before approval; `r` returns to the form, `a` approves and writes the packet, and `q` exits without approving.
- [ ] Support architect toggle: normal approval returns to Home; architect approval starts the architect route.
- [ ] Render normalized Codex events as compact thought/activity/tool rows; expand captured output and show actual changed files using split diffs.
- [ ] Route interactive `conduit refine` to this screen and retain a compact non-interactive mode.

## Acceptance criteria

- [ ] Reject and quit never create an approved feature packet.
- [ ] Approved packet contains story, QA cases, metadata, contracts, plan, and all task groups required by the selected flow.
- [ ] Architect timeline does not count repeated transcript patches twice.
- [ ] Keyboard and preview behavior have component and integration coverage.
