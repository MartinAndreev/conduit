# Contracts

Feature 006 must define these website-owned contracts during implementation:

- A single navigation-item model consumed by desktop and mobile navigation,
  with internal/external destination, active-page, and base-path semantics.
- A validated release content schema covering title, description, publication
  date, release/announcement type, optional stable version, featured state,
  optional external link, optional media metadata, and production draft
  exclusion.
- Typed roadmap phases, priorities, items, stable ordering, optional details,
  and optional dependency references.
- Progressive-enhancement state contracts for roadmap filtering/disclosure and
  drawer open/close behavior, without making server-rendered content depend on
  client state.

Contracts remain under `packages/website`; they must not import from the
publishable CLI package. Content validation must run during the static build so
invalid release or roadmap records fail before deployment.
