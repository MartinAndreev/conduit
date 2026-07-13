# Turso local-first persistence and configuration

## Outcome

Use embedded Turso locally only. Global profiles provide reusable role defaults; project configuration overrides only differences. Intermediate refinement state remains local, while only approved packet artifacts are written to Git-visible files.

## Acceptance criteria

- [ ] Configuration resolves: built-in defaults → global profile → `conduit.yml` → project role guidance.
- [ ] Global profiles persist role runner, model, effort, mode, read-only, ownership, and guidance defaults.
- [ ] `.conduit/state.turso` is ignored and safely supports multiple local Conduit writers with short transactions.
- [ ] Drafts, research, revisions, questions, answers, events, and reviews recover locally; intermediate material is not written to feature packets.
- [ ] Turso Cloud, sync, URLs, tokens, and replication are absent.
- [ ] Credentials remain in the OS vault or encrypted fallback, never plaintext database rows.
