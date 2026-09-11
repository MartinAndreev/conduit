# QA test cases

## Automated evidence

These checks are exercised by Astro validation, the static build, and
`packages/website/tests/static-output.test.mjs`. A checked item means the
corresponding automated command passed in the completion run recorded below.

- [x] Home, Docs, Releases, Roadmap, and GitHub appear in order in shared
      navigation, and Releases/Roadmap expose the active-page state.
- [x] Root-base output emits Home, Docs, Releases, authored release entries, and
      Roadmap routes.
- [x] Published release entries render in reverse-chronological archive and
      detail surfaces with article metadata; draft content is excluded.
- [x] Roadmap output contains ordered `shipped`, `development`, and `research`
      phases, and every rendered item belongs to a declared phase.
- [x] Self update, Local persistence, Universal agent communication, Durable
      role workspaces and recovery, and Approval-gated corrections render as
      shipped without planning priorities.
- [x] Agent long-term memory renders in development with high priority and names
      canonical Turso, default Memvid, optional Hindsight, and the approved
      memory contract without claiming the feature is shipped.
- [x] Linear, Jira, Asana, and ClickUp remain in research and planning; Linear
      and Jira are high priority and precede normal-priority Asana and ClickUp.
- [x] Roadmap phase counts are rendered from the same data collection as the
      cards, avoiding duplicated fixed totals in page markup.
- [x] Generated internal links resolve within root-base static output.
- [x] GitHub Pages base-path output builds successfully.
- [x] Prettier checks for supported roadmap files, Astro diagnostics/type
      checking, static-output tests, and builds pass.

## Implemented interaction contracts

These are present in the server-rendered markup, styles, and client scripts.
Static checks cover key attributes and dismissal paths, but they are not a
substitute for the manual browser matrix below.

- [x] Roadmap content is server-rendered before enhancement, while phase and
      priority controls expose `aria-pressed` and update visible phase counts.
- [x] Roadmap details use native keyboard-operable disclosure controls and
      display phase/priority status in text as well as color.
- [x] The mobile drawer includes a labeled trigger, controlled modal drawer,
      backdrop, focus management, Escape/backdrop/link dismissal, scroll lock,
      breakpoint cleanup, and reduced-motion styles.

## Manual browser checks not re-run

These checks remain part of the release QA matrix. They were not executed as
part of the roadmap content refresh, so they remain explicitly unverified here.

- [ ] At desktop widths, the three roadmap columns align, remain readable with
      long content, and do not overlap the shared navigation.
- [ ] At 320 px and with text zoom, the roadmap stacks without document-wide
      overflow and all controls meet usable touch-target dimensions.
- [ ] With JavaScript disabled, all roadmap content, labels, details, navigation
      links, and the scope disclaimer remain coherent and available.
- [ ] Phase and priority filtering, reset-to-all behavior, details disclosure,
      and focus visibility work with pointer and keyboard.
- [ ] Screen-reader output communicates current navigation, roadmap status,
      filter state, disclosure state, drawer relationships, and modal state
      without relying on color.
- [ ] Drawer focus moves in, remains trapped, returns to the trigger, and closes
      exactly once through close button, Escape, backdrop, destination, and
      desktop resize paths.
- [ ] Repeated drawer cycles leave no scroll lock, inert state, stale ARIA state,
      duplicated listeners, or obscured content.
- [ ] Reduced-motion mode removes nonessential roadmap and drawer animation.
- [ ] A static preview under `/conduit/` is manually navigated to confirm nested
      routes, fragments, canonical metadata, and assets in a browser.

## Completion run — 2026-09-11

- `pnpm --filter @conduit/website lint` — passed with no Astro diagnostics.
- `pnpm --filter @conduit/website typecheck` — passed with no Astro
  diagnostics.
- `pnpm --filter @conduit/website test` — passed all 6 static-output tests and
  its root-base static build (9 pages).
- `BASE_PATH=/conduit pnpm --filter @conduit/website build` — passed and emitted
  9 pages.
- `pnpm exec prettier --check packages/website/src/data/roadmap.ts packages/website/src/styles/global.css packages/website/tests/static-output.test.mjs specs/006-website-releases-roadmap`
  — passed.

The repository does not configure a Prettier parser for `.astro` files; Astro
syntax and types are covered by `astro check` through the package commands.
