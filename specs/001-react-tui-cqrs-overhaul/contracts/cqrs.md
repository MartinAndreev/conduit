# CQRS contract

Command and query messages are discriminated TypeScript objects. A command handler is the only mutation/side-effect owner for its message. A query handler returns a serializable immutable read model. Buses expose dispatch/execute only; screens use controller hooks rather than handlers directly.

Handler failures are typed application errors with a safe user message and diagnostic cause. Handlers are unit-tested independently of OpenTUI.
