# Tasks

## 006-A Shared shell and content contracts

- [x] Create a shared website navigation model covering Home, Docs, Releases,
      Roadmap, and GitHub with active-page and base-path-safe URL behavior.
- [x] Extract shared page shell and metadata defaults without changing current
      homepage or Docs content.
- [x] Define the validated repository-authored release content schema, draft
      exclusion, deterministic sorting, and static route contract.
- [x] Define typed roadmap phase, optional planning priority, order, detail, and
      dependency data for shipped, development, and research items.

## 006-B Release archive and entry pages

- [x] Implement `/releases/` with introduction, featured treatment,
      reverse-chronological cards, type labels, empty state, and bounded growth.
- [x] Implement `/releases/<slug>/` with long-form content, metadata, archive
      return, optional media, and optional GitHub/documentation links.
- [x] Style release typography and layouts consistently across desktop, mobile,
      long titles, code, images, and text zoom.
- [x] Add content validation, draft exclusion, sort, route, and metadata tests.

## 006-C Interactive roadmap

- [x] Implement a responsive planning-chart board grouped by ordered phase with
      visible counts, priorities, and stable item ordering.
- [x] Add shipped Self update, local persistence, universal agent communication,
      durable role workspace/recovery, and approval-gated correction entries;
      keep Agent long-term memory in development; retain Linear, Jira, Asana,
      and ClickUp in research and planning.
- [x] Add progressively enhanced phase/priority filters and accessible item
      detail disclosure without hiding server-rendered content.
- [x] Implement keyboard, no-JavaScript, reduced-motion, 320 px, long-content,
      and non-color-dependent behavior in the existing roadmap surface.

## 006-D Mobile drawer and integration

- [x] Replace the mobile `<details>` dropdown with a right-side off-canvas
      drawer and page backdrop.
- [x] Implement labeled toggle/close controls, focus trap and restoration,
      Escape/backdrop/link close, scroll lock, touch targets, and ARIA state.
- [x] Add desktop-breakpoint cleanup, enhancement-failure safety, reduced-motion
      behavior, and active-page styling.
- [x] Verify generated routes, fragments, assets, and metadata at `/` and build
      at a GitHub Pages subpath; run the website automated checks.

## Completion evidence

- Implementation is present in the shared website layout, release content and
  routes, roadmap data/page, mobile drawer, and static-output test.
- Automated verification commands and their current results are recorded in
  `test-cases.md`.
- Browser interaction at desktop and 320 px, touch behavior, focus trapping,
  reduced motion, text zoom, and assistive-technology behavior remain manual
  release checks. Checked implementation tasks do not claim those manual checks
  were re-run during this roadmap-content update.
