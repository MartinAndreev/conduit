# Conduit TUI design

## Theme tokens

The logo palette is the source of truth.

| Token              | Value                | Usage                                 |
| ------------------ | -------------------- | ------------------------------------- |
| `surface.base`     | `#20251F`            | application background                |
| `surface.raised`   | `#2B332A`            | focused controls and panels           |
| `action.primary`   | `#8FB6A0`            | selection, success, implemented state |
| `action.attention` | `#D8C28B`            | pending work, in-progress state       |
| `text.default`     | `#D8D5C8`            | body text                             |
| `text.strong`      | `#E5E1D4`            | headings and selected text            |
| `text.muted`       | `#8B8B8B`            | secondary text                        |
| `status.error`     | accessible red token | errors and not-started state          |

Only `status.error` is outside the logo palette because red communicates a required semantic state. Components consume named tokens, never literal color values.

## Screens

- **Home:** feature search/sidebar, lifecycle indicators, selected-feature actions, welcome/refinement entry, random tip, and role portraits.
- **Refinement:** tabbed multi-field input, preview, draft recovery, approval/rejection, and architect toggle.
- **Architect activity:** normalized compact event timeline, expandable output, and split diff viewer.
- **Worker run:** role progress, compact event timeline, file changes, cancellation, and split diffs.
- **Feature details:** packet, metadata, task group, run, and review read model.

## Interaction

Tab and Shift+Tab move form focus. Ctrl+Enter submits a refinement form. Sidebar search has a documented global focus shortcut selected during implementation. `q` closes the active modal/view; Ctrl+C cancels an active run only after confirmation. Narrow terminals show an actionable minimum-size message rather than a broken layout.
