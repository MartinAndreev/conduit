# Feature Package Hash Contract

The package hash is SHA-256 over approved package inputs only:

- `story.md`
- `spec.md`
- `plan.md`
- `tasks.md`
- `test-cases.md`
- `contracts/**`
- explicit role ownership/configuration files supplied by the caller

Paths are sorted lexicographically and encoded before content. Content line endings are normalized to LF. Raw transcripts, timestamps, run IDs, temporary files, generated questions, and unrelated database rows are excluded.
