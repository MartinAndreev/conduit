# Communication Provider V1 Contract

`AgentCommunicationProvider` is the only runtime boundary visible to orchestration. It is harness-agnostic, role-agnostic, and transport-agnostic. Native ACP, RPC, JSON-RPC, app-server, JSONL, SDK, CLI, and HTTP/SSE details are provider internals.

Responsibilities:

- inspect harness availability, version, selected protocol, final-response strategy, and telemetry capabilities;
- create or verify a native session for a supplied Feature 007 `AgentAssignmentV1`;
- stream normalized `ConduitRuntimeEventV1` values through an `AsyncGenerator`;
- return a typed terminal result from generator completion;
- accept bidirectional permission responses where supported;
- request native cancellation before bounded process-group termination;
- redact and bound all payloads before yielding;
- cleanup protocol and process resources in `finally` paths.

Consumers must use the explicit `next()` loop so persistence is awaited before the next event is consumed. `for await` is not used for production persistence because it hides the generator return value.
