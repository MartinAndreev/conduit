# Feature 006: Website releases, roadmap, and mobile navigation drawer

## Outcome

Add Releases and Roadmap to the Conduit website's primary navigation. Releases
is a static, blog-style publication surface for regular releases and major
release announcements. Roadmap is an interactive planning chart showing work
by phase and priority without inventing delivery dates. Replace the current
mobile dropdown with an accessible off-canvas navigation drawer.

This feature is contained within `packages/website`. It does not change the
Conduit CLI, release binaries, updater behavior, or feature packet contracts.

## Information architecture

The primary navigation includes stable, base-path-safe links to:

- Home
- Docs
- Releases
- Roadmap
- GitHub

Homepage section links may remain available where useful, but the header must
not become crowded at supported desktop widths. Every page uses the shared
header and identifies the current page with visible styling and
`aria-current="page"`.

Required routes:

- `<base>/releases/` — release archive
- `<base>/releases/<slug>/` — individual release or announcement
- `<base>/roadmap/` — interactive roadmap

All internal URLs, assets, canonical metadata, and generated links must work
both at `/` and under the configured GitHub Pages base path.

## Releases

### Archive

The Releases page uses an editorial, blog-like layout consistent with the
existing Conduit visual language. It includes:

- a page introduction explaining what is published;
- a clearly featured newest or explicitly featured major announcement;
- reverse-chronological release cards with title, version when applicable,
  publication date, summary, type, and link to the full entry;
- a visible distinction between regular release notes and major announcements;
- an intentional empty state before the first authored entry; and
- responsive pagination or bounded archive rendering if the entry count grows.

Dates are rendered from machine-readable ISO values in a consistent human
format. Sorting is deterministic; featured presentation does not duplicate the
same entry in the archive list unless the design labels that duplication.

### Individual entries

Each entry renders readable long-form content with:

- title, description, publication date, content type, and optional version;
- optional hero/cover media with meaningful alt text;
- headings, paragraphs, lists, links, code blocks, and callouts;
- a link back to the release archive;
- optional links to the GitHub release and relevant documentation; and
- page-specific title, description, canonical URL, and social metadata.

Release content is authored as repository-owned Astro content, Markdown, or
MDX rather than embedded in page components. A schema validates at least:

- `title`
- `description`
- `publishedAt`
- `type`: `release` or `announcement`
- optional stable SemVer `version`
- optional `featured`
- optional `githubReleaseUrl`
- optional cover image and alt text
- draft state that is excluded from production output

Version text and external links are content, not an automated mirror of GitHub
Releases. This packet does not introduce a CMS, remote API, or deployment-time
GitHub dependency.

## Roadmap

### Planning-chart presentation

The Roadmap page looks and behaves like a product planning chart rather than a
generic card grid. On wide screens it presents ordered phase columns with clear
headers, counts, and aligned work items. On narrow screens the same information
becomes a readable stacked or horizontally scrollable board without shrinking
text below accessible sizes.

Initial phases and required items are:

| Phase               | Item                | Priority |
| ------------------- | ------------------- | -------- |
| In development      | Self update         | High     |
| In development      | Agent memory        | High     |
| Research & planning | Linear integration  | High     |
| Research & planning | Jira integration    | High     |
| Research & planning | Asana integration   | Normal   |
| Research & planning | ClickUp integration | Normal   |

The chart may include a future/released phase for layout continuity, but it
must not falsely claim an item or delivery date. Linear and Jira are visually
and semantically identified as higher priority than Asana and ClickUp. Order
within a phase is explicit and stable.

Each roadmap item has a title, concise outcome, phase, priority, stable order,
and optional details or dependency references. Roadmap data lives in one typed,
validated website-owned source so content can move between phases without
rewriting page markup.

### Interaction

- Visitors can filter or highlight roadmap items by phase and priority.
- Selecting a work item expands an inline detail region or opens an accessible
  non-navigational detail panel with its outcome, current phase, priority, and
  known dependencies.
- The initial view contains all items and remains understandable with client
  JavaScript unavailable. Interactivity progressively enhances server-rendered
  content rather than creating the content itself.
- Controls are keyboard operable, have visible focus, expose selected/expanded
  state to assistive technology, and do not rely on color alone.
- Filters must not encode or imply committed dates. The page includes a concise
  note that roadmap order and scope may change as research develops.
- Motion used to rearrange, reveal, or emphasize items respects
  `prefers-reduced-motion`.

The initial dependency note for Agent memory may reference its persistence
foundation when useful, but the website must not expose internal implementation
claims that are not approved for public communication.

## Mobile navigation drawer

Replace the current `<details>` dropdown behavior at the mobile breakpoint with
a true off-canvas drawer:

- A labeled menu button exposes `aria-expanded` and `aria-controls`.
- Opening it reveals a full-height drawer from the right and a page-covering
  backdrop. The drawer is visually separate from the header dropdown pattern.
- The drawer contains the same destination set and order as desktop navigation,
  including Releases, Roadmap, and GitHub.
- Focus moves into the drawer on open, remains trapped among its interactive
  controls, and returns to the menu button on close.
- Escape, the close button, backdrop activation, and choosing a destination all
  close the drawer.
- Background scrolling and pointer interaction are blocked while open. The
  currently active page is identified.
- A resize to the desktop breakpoint closes the drawer and restores document
  state without leaving scroll locks, hidden content, or stale ARIA values.
- Drawer transitions respect `prefers-reduced-motion`.
- The control works with touch, pointer, and keyboard input. The close target
  and navigation rows meet reasonable mobile touch-target dimensions.

The server-rendered page remains navigable if enhancement fails: primary links
must not be removed from the document solely by JavaScript, and enhancement
must not leave the page permanently obscured.

## Shared website architecture

- Extract or extend shared page shell, metadata, and header behavior so the
  homepage, Docs, Releases, release entries, and Roadmap do not duplicate
  navigation markup or head defaults.
- Navigation destinations come from one website-owned source consumed by the
  desktop and mobile views.
- Prefer Astro and small framework-free client scripts. Do not add a frontend
  framework solely for the roadmap or drawer.
- Page and component behavior stays inside `packages/website`; there is no
  runtime import from `packages/conduit`.
- Reuse the existing palette, typography, spacing language, and design tokens.
  New page styles must not introduce an unrelated visual system.
- Interactive scripts must clean up global listeners and document state across
  Astro client navigation if that capability is enabled later.

## Accessibility and quality

- Pages use one logical `h1`, ordered heading levels, landmarks, descriptive
  link text, and valid HTML.
- All functionality is available by keyboard with visible focus indicators.
- Drawer and roadmap state have appropriate accessible names and relationships;
  announcements avoid noisy live-region updates.
- Text and state indicators meet WCAG AA contrast. Phase and priority are
  communicated with labels or icons as well as color.
- Layouts support at least 320 px wide screens, text zoom, long titles, and
  content reflow without horizontal page overflow. A deliberately scrollable
  roadmap region is labeled and does not cause document-wide overflow.
- Static output contains no draft releases, broken internal links, invalid
  content records, or dependency on a live backend.

## Acceptance criteria

- [ ] Shared desktop and mobile navigation link to Home, Docs, Releases,
      Roadmap, and GitHub with correct base-path handling and current-page state.
- [ ] `/releases/` renders a blog-style reverse-chronological archive with a
      featured treatment, typed entries, useful empty behavior, and entry links.
- [ ] `/releases/<slug>/` renders validated repository-authored release or major
      announcement content with correct metadata and draft exclusion.
- [ ] `/roadmap/` renders the six required items in a planning-chart layout with
      Self update and Agent memory in development, all integrations in research
      and planning, and Linear/Jira at higher priority.
- [ ] Roadmap filtering and item details are progressively enhanced, keyboard
      accessible, non-color-dependent, and usable at mobile widths.
- [ ] Mobile navigation is an off-canvas drawer with backdrop, focus management,
      Escape/backdrop/link closing, scroll lock, resize cleanup, and reduced
      motion support.
- [ ] Homepage, Docs, Releases, entries, and Roadmap share navigation behavior
      without importing CLI runtime code or requiring a remote backend.
- [ ] Astro checks and static builds pass at root and GitHub Pages base paths.

## Non-goals

- Automatically publishing release entries from tags or GitHub Releases.
- A CMS, author accounts, comments, likes, search service, analytics, RSS/email
  delivery, or release subscription system.
- Committed roadmap dates, percentage-complete estimates, issue-tracker sync,
  drag-and-drop editing, or public roadmap voting.
- Implementing Self update, Agent memory, or any project-management provider
  integration in the CLI.
- Redesigning the homepage, documentation content, logo, or overall brand.
