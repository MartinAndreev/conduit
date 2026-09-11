# Contracts

Feature 006 must define these website-owned contracts during implementation:

- A single navigation-item model consumed by desktop and mobile navigation,
  with internal/external destination, active-page, and base-path semantics.
- A validated release content schema covering title, description, publication
  date, release/announcement type, optional stable version, featured state,
  optional external link, optional media metadata, and production draft
  exclusion.
- Typed roadmap phases (`shipped`, `development`, and `research`), items, stable
  ordering, optional planning priorities, optional details, and optional
  dependency references. Development and research items require a planning
  priority; shipped items omit one.
- Progressive-enhancement state contracts for roadmap filtering/disclosure and
  drawer open/close behavior, without making server-rendered content depend on
  client state.

Contracts remain under `packages/website`; they must not import from the
publishable CLI package. Content validation must run during the static build so
invalid release or roadmap records fail before deployment.

## Approved roadmap content

The roadmap content contract currently contains five shipped capabilities, one
in-development design, and four researched integrations. The source of truth is
`packages/website/src/data/roadmap.ts`; phase counts are derived from that source
rather than duplicated in page markup or tests.

Shipped claims are bounded by their public details and release compatibility
gates. Agent long-term memory remains in development. Its current direction is
canonical Turso storage, a default recoverable Memvid index, an optional
configurable Hindsight adapter, and provider-independent communication. This is
roadmap intent until the approved memory contract is implemented.
