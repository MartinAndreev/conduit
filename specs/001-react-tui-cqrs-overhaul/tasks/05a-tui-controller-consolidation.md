# Group 5.5 — TUI controller consolidation

## Outcome

Make TUI state transitions explicit, remove duplicated polling, selection, and diff rendering behavior, and restore the presentation boundary before accepting Group 5 worker monitoring. This corrective group does not begin SQLite persistence or add provider integrations.

## Scope

- Consolidate related `useState` collections into reducers where they model one interaction or screen lifecycle.
- Extract reusable TUI hooks and view components where Group 4 and Group 5 duplicate behavior.
- Move controller view-model contracts out of controller implementation files.
- Delete duplicated worker-monitor views and retain one route/screen composition.
- Preserve existing keyboard shortcuts and refinement/worker behavior while making focus ownership explicit.

## Required implementation

### Shared TUI contracts

Create dedicated TUI type modules. Do not declare exported view-model, reducer-state, action, or focus-mode interfaces in a controller implementation file.

| Contract           | Target location                                             | Required content                                                                                             |
| ------------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Worker monitor     | `src/tui/types/worker-monitor.ts`                           | `RoleViewModel`, `WorkerMonitorViewModel`, reducer state/actions, focus mode, selected role/file/event state |
| Architect activity | `src/tui/types/architect-activity.ts`                       | activity state, file selection, expansion state, loading/error read model                                    |
| Refinement UI      | `src/tui/types/refinement.ts`                               | refinement view union, controller state/actions, packet-content view type                                    |
| Shared selection   | `src/tui/types/selection.ts` only if both consumers need it | selected-index/focus contracts only                                                                          |

`RunnerEvent`, changed-file data, review findings, and other business values remain in their owning runs-domain type modules. Do not create a global type directory or barrel file.

### Reusable polling and selection hooks

Add focused reusable hooks under `src/tui/hooks/` (or `src/tui/controllers/` if they are controller-only), with their own tests.

#### `usePollingQuery`

- Accept a query executor, query factory, interval, enabled flag, and result projection callback.
- Execute once immediately when enabled, then poll at the requested interval.
- Clear the interval on disable/unmount and ignore stale async responses after cleanup.
- Return a consistent loading/error/reload surface without knowing about architect events, runner events, or navigation.
- Replace duplicated polling in architect activity and worker monitoring.

#### `useSelectableList`

- Accept item count, selected index, and selection callback.
- Provide bounded or cyclic `next`, `previous`, and `reset` behavior selected by an explicit option.
- Reset safely when the item count shrinks to zero.
- Use it for architect changed-file selection and worker role/file selection; do not embed keyboard bindings in the hook.

### View-owned split diff

Create `src/tui/components/SplitDiff.tsx`.

- It owns `useRenderer`, `DiffRenderable`, `BoxRenderable` cleanup, syntax styling, line numbers, split view, and scroll synchronization.
- It receives only `diff`, height, optional id, and presentation options as props.
- It renders an empty-state message when no selected-file diff exists.
- Remove all OpenTUI renderer primitives, refs, dynamic imports, and cleanup from `useArchitectActivityController` and `useWorkerMonitorController`.
- Reuse it in architect activity and the retained worker monitor. Legacy compact dashboard code may remain separate until migrated, but must not gain new duplicate diff behavior.

### Worker monitor state machine and focus

Keep one `WorkerMonitorScreen` and one `RunScreen` composition. Delete the duplicate unused monitor screen.

| Focus      | Navigation                 | Enter/Space                                 | Escape/q        |
| ---------- | -------------------------- | ------------------------------------------- | --------------- |
| `roles`    | Select a role              | Move to activity/files detail for that role | Leave monitor   |
| `files`    | Select changed file        | Toggle selected-file split diff             | Return to roles |
| `activity` | Scroll transcript/timeline | Expand/collapse selected transcript event   | Return to roles |

- `selectedFileIndex` is reducer state, never hard-coded in a screen.
- A selected file renders only that file’s unified diff; never pass a complete role diff to `SplitDiff` for a file selection.
- Role state/message projection is a pure reusable function or a runs query projection; do not duplicate it in screen/component/controller files.
- `Ctrl+C` dispatches run cancellation once and updates the UI only from its command result or persisted lifecycle event.
- `q` exits the monitor without cancellation.

### Reducer migrations

#### Home

Replace coupled booleans in `useHomeController` with this interaction state:

```ts
type HomeInteraction =
  | { kind: "idle" }
  | { kind: "search"; query: string }
  | { kind: "create"; title: string }
  | { kind: "featureActions"; actionIndex: number };
```

- Search, creation, and feature actions are mutually exclusive.
- Escape returns to `idle` and clears a draft search/create value only where existing behavior does.
- Feature selection, loaded data, and random tip may remain independent if they are not part of interaction transitions.

#### Refinement

Replace coupled lifecycle state in `useRefinementController` with a reducer that owns at least:

```ts
type RefinementView =
  | "loading"
  | "form"
  | "packet"
  | "preview"
  | "architect"
  | "clarifications"
  | "review"
  | "error";
```

- Architect status comes from one lifecycle field: `idle | running | cancelled | failed`, never an independently mutable boolean.
- Completed architect results transition only to `clarifications` or `review`.
- Cancellation returns to the prior non-running view only after its command completes.
- Packet/revision refreshes must not replace an active form with packet view.

#### Architect activity

Use a reducer or the shared hooks to keep events, derived files, selection, expansion, loading, and error transitions atomic. Its controller contains no rendering primitives.

### Tests

Add focused tests for:

- polling executes immediately, stops on disable/unmount, clears timers, and ignores stale responses;
- selectable-list bounded/cyclic behavior and empty-list reset;
- Home interaction mode never exposes search/create/actions concurrently;
- refinement lifecycle transitions, including architect completion, clarification, cancellation, and error;
- worker focus transitions, selected-file diff extraction, `q` without cancellation, and one cancellation dispatch for `Ctrl+C`;
- split-diff empty state/lifecycle through an appropriate OpenTUI seam;
- no controller imports `@opentui/core` renderer primitives or declares exported view-model contracts;
- only one `WorkerMonitorScreen` implementation remains.

Run `pnpm format`, `pnpm typecheck`, `pnpm lint`, `pnpm test`, `pnpm build`, `pnpm build:standalone linux-x64`, and `pnpm start --help`.

## Non-goals

- SQLite/Kysely migration or changing Group 6 persistence decisions.
- Replacing the command/query buses.
- Remote feature providers or credential changes.
- Altering the approved feature-packet/revision-loop contract.
