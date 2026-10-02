## Domain-driven organization

- Organize behavior by its owning domain and business language. Keep domain rules independent of concrete harness, process, database, and presentation implementations.
- Domain code owns business identities, value types, rules, and valid transitions. Application code composes use cases through procedures and ports. Infrastructure implements concrete effects and reads. Presentation invokes use cases and renders outcomes.
- Do not create global catch-all directories for all rules, steps, or procedures across unrelated domains. These are responsibilities, not a requirement for one file, package, or interface per function.
- A service exposes related procedures; it must not become a second owner of their business flow.
- Share code only when its semantics and ownership are shared. Similar syntax alone does not justify a shared package. A technical ID generator is not a domain model; distinguish domain, adapter-local, and native harness identities.
- Keep raw database connections inside storage infrastructure. Application behavior uses ports/adapters and repositories rather than direct connection access.
- Exact domain boundaries, Go directory layout, interface placement, and framework/library choices remain subjects for explicit design discussion. Do not infer them from TypeScript conventions or this vocabulary.

## Composition vocabulary

### Rule

- A rule answers a named business or technical policy question from supplied facts. It is deterministic for those facts, performs no I/O, and does not mutate state.
- Return a concrete decision structure for that specific question, containing a typed `Kind` and explanatory `Reason`. Define the permitted kinds for that question rather than introducing a universal result for all rules.
- Procedures branch on `Kind`, never on the text of `Reason`. `Reason` explains the decision for people, logs, UI, and agent feedback.
- Keep business rules in their owning domain and technical rules in their owning infrastructure/adapter package. A technical rule does not become domain logic merely because it returns a decision. Do not create a global rules package.
- Approved technical-rule layout: each owning Codex, OpenCode, eventstream, and Turso package keeps its rules and their Facts/Decision/Kind types in its own `rules` subpackage. Pure rule tests live alongside those rules; procedure/integration tests stay with their owner. Dependency direction is owner → rules; rules must not import the parent adapter, mutable session, process, or database implementation. Keep `sessions.DecideCorrection` in its existing business owner; no global rules package or unrelated domain move.
- An expected policy rejection is a decision. The procedure translates it into the appropriate public error/result and preserves any original technical cause. The rule neither performs the operation nor formats its transport response. Define handling of invalid facts explicitly instead of inventing a policy.
- Extract a named rule when a branch expresses a meaningful eligibility, validity, or transition policy, including technical lifecycle policy. Duplication is not a prerequisite. Do not mechanically extract every `if`: direct error propagation, native protocol decoding, and trivial defensive checks need no decision abstraction unless they encode such a policy.
- Evaluate rules from explicit facts, not mutable owner access or hidden callbacks. Fact reads, the decision, and dependent mutation/attempt must stay in the same existing critical section when required for atomicity; a pure rule does not acquire locks itself.
- Approved example: eventstream `DecidePublish` returns `PublishDecision{Kind, Reason}` with `PublishAllowed`, `PublishConsumerClosed`, or `PublishStreamClosed`. Consumer closure takes precedence over stream closure. `Publish` maps a stream-closed decision to the stored terminal error or `io.ErrClosedPipe`; the rule need not interpret that error. Both the decision and the nonblocking send stay under the broker mutex. Allowed means eligible to attempt publishing, not guaranteed success: the attempt may still report buffer overflow.

### Query

- A query gathers facts through the relevant read port without changing business state. It may perform I/O and fail technically.
- Keep business decisions in rules and flow ordering in procedures; do not hide them inside repository reads.

### Step

- A step performs one named logical side effect and returns its outcome or failure. It does not own the overall flow.
- One logical effect may require several technical operations, such as persisting one aggregate across multiple tables. Combining persistence, starting an agent, and sending a notification is several effects, not one step.
- Make effect, inputs, outcome, and failure semantics explicit. Retry safety and uncertain delivery must be addressed where relevant; never imply every step is idempotent.

### Procedure

- A procedure owns one use case's flow in one place: gather facts with queries, evaluate rules, decide the path, and invoke steps in order. Repeat those phases when the use case requires it.
- Keep business sequencing visible. Do not duplicate the flow in services, handlers, UI, or adapter code.
- Give multi-action success, failure, and reconciliation paths explicit names. When a branch classifies an outcome, changes state, and constructs a response, extract a focused subprocedure so the caller shows the overall flow. A simple guard or error propagation need not become a separate function.
- A subprocedure owns its sequence; its name and contract must expose effects and uncertainty. Do not hide state changes behind a name that implies pure error formatting, or move technical protocol policy into a business domain merely to match the vocabulary. Pure technical policy may be a local rule; parsing and direct error propagation alone do not require one.
- Describe partial-success behavior and transaction/concurrency boundaries before implementing a multi-effect procedure. A database transaction cannot undo a command already sent to a harness.

### Wrapper

- A wrapper decorates execution, for example `withLock`, `withTransaction`, or `withRetry`. It must not hide business decisions or become an alternate procedure.
- Reuse an established wrapper for repeated lock/callback/unlock mechanics within its owning technical boundary. A mutex wrapper requires a synchronous callback, acquires the lock, and defers its release so early returns and panics release it; it must not swallow a panic or add retry/recovery behavior.
- Keep callbacks short and state-focused. Related fact reads, validation, and mutation/reservation stay within one critical section. Do not split them into separately locked helpers, nest acquisition of the same mutex, or perform harness I/O/event publication under the session mutex.
- Keep domain-specific state transitions in named steps/subprocedures; the wrapper owns only synchronization. Do not invent optional callbacks or generic workflow abstractions without an actual use case. Existing unrelated flows are migrated only within an approved task scope.
- Specify wrapper ordering and scope where they change behavior. Retrying an entire procedure can repeat external effects; do not apply retries without an agreed delivery/idempotency policy.
- Cancellation can end waiting without undoing a delivered request. Preserve uncertainty rather than assuming remote cancellation.

### Skeleton

- A skeleton is a shared procedure shape extracted from demonstrated common flow in concrete procedures, with explicit variation points.
- Introduce it only after the common ordering and guarantees are understood. Prefer composition; do not introduce inheritance emulation or a general workflow engine merely to support this pattern.
- Preserve one owner for the shared flow and keep procedure-specific business decisions visible.
