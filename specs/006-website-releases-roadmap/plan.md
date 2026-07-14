# Plan: Website releases, roadmap, and mobile navigation drawer

## Delivery sequence

### 006-A Shared shell and content contracts

- Define one base-path-safe navigation model and shared page metadata/shell.
- Define and validate the release content schema, draft behavior, sorting, and
  route generation.
- Define the typed roadmap phases, priorities, item schema, initial content,
  and stable ordering.
- Add fixtures for invalid content and GitHub Pages base-path behavior.

### 006-B Release archive and entry pages

- Build the editorial Releases archive, featured treatment, entry cards, and
  empty/large-archive behavior.
- Build static release-entry routes and long-form content presentation.
- Add page-specific canonical and social metadata, external release links, and
  responsive content styling.
- Add initial authored content only if separately approved; placeholders must
  not masquerade as published releases.

### 006-C Interactive roadmap

- Build the responsive phase-column planning chart with the six approved items.
- Add progressively enhanced phase/priority controls and accessible item detail
  disclosure.
- Add mobile chart behavior, visible labels, focus states, reduced-motion
  behavior, and the roadmap-change disclaimer.
- Verify the complete roadmap remains meaningful without JavaScript.

### 006-D Mobile drawer and integration

- Replace the mobile `<details>` dropdown with an off-canvas drawer using the
  shared navigation model.
- Add backdrop, focus movement/trap/return, close paths, scroll lock, ARIA state,
  breakpoint cleanup, touch targets, and reduced-motion handling.
- Integrate the shared header across all website pages and verify active-page
  state and homepage fragment destinations from nested routes.
- Run accessibility, responsive, link, Astro, static-build, and Pages-base-path
  regression checks.

## Delivery gate

Each task group requires review and acceptance before the next begins. Work is
limited to `packages/website`, this packet, and directly related website tests
or documentation. Do not implement CLI roadmap items as part of this feature.

## Verification commands

- `pnpm format:check`
- `pnpm lint`
- `pnpm typecheck`
- `pnpm test`
- `pnpm --filter @conduit/website lint`
- `pnpm --filter @conduit/website build`
- `BASE_PATH=/conduit pnpm --filter @conduit/website build`
- Static preview and browser interaction checks at desktop and 320 px mobile
  widths
