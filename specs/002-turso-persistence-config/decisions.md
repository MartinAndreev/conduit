# Decisions

- Embedded `@tursodatabase/database` remains the selected engine for project and user-global local persistence.
- The project database lives at `<project>/.conduit/state.db` by default; the global database lives in the platform app-data directory.
- The Conduit process owns all database connections. Agents never open, query, mutate, or receive either database.
- Direct multi-process database coordination and experimental Turso multi-process WAL are not design dependencies.
- Configuration precedence is built-in defaults, user-global profile, `conduit.yml`, then project role guidance.
- Credentials remain outside databases in the OS credential vault or existing encrypted fallback.
- Bun standalone native-binding packaging is a required compatibility gate for release artifacts.
- Feature 002 provides persistence primitives only; Feature 003 owns memory lifecycle, retrieval, context compilation, and promotion.
