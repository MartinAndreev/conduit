# Database Ownership Contract

The project `state.db` is the mutable source of truth for Feature 008 runtime state. Required canonical tables are:

- `feature_package_versions`
- `harness_sessions`
- `harness_turns`
- `clarification_questions`
- normalized runtime events
- final result records
- diagnostic artifact metadata
- legacy import ledger

Approved Git-backed package files under `specs/` remain authoritative package artifacts. Raw transcripts, launch files, run directories, and legacy mutable Markdown/JSON files are never authoritative context.
