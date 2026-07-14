# QA test cases

## Navigation and shared shell

- [ ] Home, Docs, Releases, Roadmap, and GitHub appear in the same order in
      desktop and mobile navigation.
- [ ] Internal links and homepage fragments resolve correctly from `/`, Docs,
      Releases, an individual release, and Roadmap at both root and `/conduit/`
      base paths.
- [ ] The active page is visibly marked and exposes `aria-current="page"`.
- [ ] Header layout remains usable at supported desktop widths without overlap,
      clipping, or unintended wrapping.

## Release content and archive

- [ ] Valid release and announcement entries build; missing required fields,
      invalid dates/types/versions, duplicate slugs, and invalid cover metadata
      fail with actionable content errors.
- [ ] Draft entries are absent from archive output, generated routes, metadata,
      and production assets.
- [ ] Entries sort deterministically newest first across time zones and equal
      dates; featured content does not create an unlabeled duplicate.
- [ ] The archive distinguishes regular releases and major announcements using
      text as well as visual treatment.
- [ ] Empty content has an intentional empty state; a large fixture uses the
      approved bounded archive or pagination behavior.
- [ ] Individual entries render supported long-form elements, back navigation,
      optional links/media, unique title/description, canonical URL, and social
      metadata.
- [ ] Long titles, code blocks, large images, missing optional fields, text
      zoom, and a 320 px viewport do not cause document-wide overflow.

## Roadmap content and planning chart

- [ ] Self update and Agent memory render in `In development` with high
      priority.
- [ ] Linear, Jira, Asana, and ClickUp integrations render in
      `Research & planning`; Linear and Jira are high priority and ordered ahead
      of the normal-priority Asana and ClickUp items.
- [ ] The wide layout reads as ordered phase columns with counts and aligned
      items rather than an undifferentiated card grid.
- [ ] The mobile layout preserves phase, priority, ordering, details, and labels
      without unreadably shrinking the board or overflowing the document.
- [ ] All roadmap content and the change disclaimer remain visible and coherent
      with JavaScript disabled.
- [ ] Phase and priority controls filter/highlight predictably, expose state to
      assistive technology, and provide a clear way to restore all items.
- [ ] Item details open and close with pointer and keyboard, expose expanded
      state, retain logical focus, and never rely on color alone.
- [ ] Reduced-motion mode removes nonessential board and disclosure animation.

## Mobile drawer

- [ ] At the mobile breakpoint the dropdown is replaced by a labeled menu
      button, full-height right drawer, backdrop, explicit close control, and
      mobile-sized navigation rows.
- [ ] Opening updates `aria-expanded`, associates the controlled drawer, moves
      focus inside, locks background scroll, and blocks background interaction.
- [ ] Tab and Shift+Tab remain within the open drawer; close restores focus to
      the menu button.
- [ ] Close button, Escape, backdrop activation, and destination selection each
      close the drawer and restore document state exactly once.
- [ ] Resizing to desktop while open closes the drawer, removes scroll lock and
      inert/hidden state, and leaves desktop navigation operable.
- [ ] Repeated open/close cycles do not duplicate listeners or leave stale
      classes, styles, ARIA values, or focus state.
- [ ] Reduced-motion mode avoids drawer animation while retaining backdrop and
      state changes.
- [ ] With enhancement unavailable, navigation links remain present and the
      page is not obscured or keyboard-trapped.

## Regression and build gates

- [ ] Homepage sections and Docs anchors remain reachable from the shared
      navigation and retain their existing content.
- [ ] No website module imports CLI runtime code or requires a live backend,
      GitHub API, CMS, or browser storage.
- [ ] Formatting, lint, typecheck, tests, Astro check, root static build, and
      GitHub Pages base-path static build pass.
- [ ] Static output has no broken internal links, missing referenced assets, or
      draft release content.
