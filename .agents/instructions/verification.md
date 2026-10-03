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

## Review corrections: required evidence

- Read the actual entry procedures and explain their main success and consequential failure paths. Check that low-level I/O and public result mapping have clear owners. Passing tests, shorter functions, and fewer `if` statements do not establish readability.
- Search changed packages for raw `.Lock()`/`.Unlock()` pairs, local `withLock` implementations, and old shared-helper imports. Confirm that ordinary synchronous scopes use `internal/lockscope`, including new buffers, mailboxes, and process owners. Inspect critical-section boundaries after reuse; do not accept a wrapper that changes which facts and mutations are atomic.
- When supported variants change or are reorganized, inspect ID lists, hard-coded counts, condition chains, and parser dispatch together. Verify descriptor/schema coverage and unknown-ID rejection without silently broadening the schema.
- For file-query corrections, exercise observable distinctions: initially absent, unreadable, non-regular/symlink, open failure after lookup, original Stat error, identity mismatch, bounded-size rejection, parsing/future schema, and cancellation. Assert preserved causes as well as result kind.
- For publication refactors, verify write/sync/close failures remove only the operation's own temporary file and leave no partial target. Retain the distinction between pre-publication failure and `Published` with a later sync error, including simultaneous cancellation.
- For process lifecycle corrections, test cancellation during lookup, successful and failed root exit with descendants, kill failure with delayed reap, slow or failing pipe closure, bounded caller return, repeated cleanup requests, concurrent observer reads, and eventual completion after the caller timed out. Fixtures must release their blocked processes/readers and verify their own cleanup.
- Compare the final code with the reviewed snapshot and state any residual limits. Cross-compilation is not native execution; green focused tests do not substitute for the required integrated suite or for architectural review.
