# Verification and review

- Review changes against the approved feature contract and the applicable architecture instructions. Passing tests alone do not establish architectural acceptance.
- Inspect both business and technical policy decisions using the Rule definition in architecture.md. Look beyond duplicate code: identify meaningful eligibility/transition decisions embedded in procedures, while distinguishing them from direct error propagation and protocol parsing. Preserve decision/error separation, policy ownership, and atomic read/decide/act scopes.
- Trace success, failure, and reconciliation paths equally. Check that multi-action branches are named subprocedures, repeated synchronization uses the relevant wrapper, and extraction preserves lock boundaries, visible effects, and error/delivery semantics. Do not accept function extraction solely because it reduces line count.
- Run checks appropriate to the changed behavior and the active task's requirements. Report the commands actually run, results, limitations, and unresolved decisions. Do not claim unfinished migration work is delivered.
- A review correction identifies the violated contract/rule, affected files, intended behavior, and exact verification. After a second failed review, stop broad retries and explicitly narrow the remaining work with Martin; do not bypass the Sol implementation agreement.
- Handoffs briefly state changed behavior, purpose, files, validation, and remaining risks. Explain relevant concepts when teaching is part of the task; it is not mandatory to add a tutorial to every handoff.
- Do not commit unless explicitly requested. Preserve unrelated changes.

## TUI visual changes

For implemented or modified TUI screens, use the registered `tui-visual-audit` skill with the relevant screen reference and an actual application capture before handoff. Record missing runnable UI/capture or an unapproved baseline as a verification limitation. A generated mockup alone is design evidence, not a passed visual check. This adds visual evidence to the existing review flow without authorizing live harness calls or automatic fixes.
