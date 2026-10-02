# Verification and review

- Review changes against the approved feature contract and the applicable architecture instructions. Passing tests alone do not establish architectural acceptance.
- Trace success, failure, and reconciliation paths equally. Check that multi-action branches are named subprocedures, repeated synchronization uses the relevant wrapper, and extraction preserves lock boundaries, visible effects, and error/delivery semantics. Do not accept function extraction solely because it reduces line count.
- Run checks appropriate to the changed behavior and the active task's requirements. Report the commands actually run, results, limitations, and unresolved decisions. Do not claim unfinished migration work is delivered.
- A review correction identifies the violated contract/rule, affected files, intended behavior, and exact verification. After a second failed review, stop broad retries and explicitly narrow the remaining work with Martin; do not bypass the Sol implementation agreement.
- Handoffs briefly state changed behavior, purpose, files, validation, and remaining risks. Explain relevant concepts when teaching is part of the task; it is not mandatory to add a tutorial to every handoff.
- Do not commit unless explicitly requested. Preserve unrelated changes.
