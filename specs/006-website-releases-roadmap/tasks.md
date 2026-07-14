# Tasks

## 006-A Shared shell and content contracts

- [ ] Create a shared website navigation model covering Home, Docs, Releases,
      Roadmap, and GitHub with active-page and base-path-safe URL behavior.
- [ ] Extract shared page shell and metadata defaults without changing current
      homepage or Docs content.
- [ ] Define the validated repository-authored release content schema, draft
      exclusion, deterministic sorting, and static route contract.
- [ ] Define typed roadmap phase, priority, order, detail, and dependency data
      with all six approved initial items.

## 006-B Release archive and entry pages

- [ ] Implement `/releases/` with introduction, featured treatment,
      reverse-chronological cards, type labels, empty state, and bounded growth.
- [ ] Implement `/releases/<slug>/` with long-form content, metadata, archive
      return, optional media, and optional GitHub/documentation links.
- [ ] Style release typography and layouts consistently across desktop, mobile,
      long titles, code, images, and text zoom.
- [ ] Add content validation, draft exclusion, sort, route, and metadata tests.

## 006-C Interactive roadmap

- [ ] Implement a responsive planning-chart board grouped by ordered phase with
      visible counts, priorities, and stable item ordering.
- [ ] Place Self update and Agent memory in development; place Linear, Jira,
      Asana, and ClickUp integrations in research and planning; mark Linear and
      Jira high priority.
- [ ] Add progressively enhanced phase/priority filters and accessible item
      detail disclosure without hiding server-rendered content.
- [ ] Add keyboard, no-JavaScript, reduced-motion, 320 px, long-content, and
      non-color-dependent state tests.

## 006-D Mobile drawer and integration

- [ ] Replace the mobile `<details>` dropdown with a right-side off-canvas
      drawer and page backdrop.
- [ ] Implement labeled toggle/close controls, focus trap and restoration,
      Escape/backdrop/link close, scroll lock, touch targets, and ARIA state.
- [ ] Add desktop-breakpoint cleanup, enhancement-failure safety, reduced-motion
      behavior, and active-page styling.
- [ ] Verify all routes, fragments, assets, metadata, and interactions at `/`
      and a GitHub Pages subpath; run the full workspace checks.
