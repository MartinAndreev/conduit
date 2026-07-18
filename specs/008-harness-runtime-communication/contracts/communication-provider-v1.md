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

Each submitted assignment is one validated workflow turn with one terminal result. Reviewer correction orchestration remains outside providers. A correction or re-review may open a new provider session with a unique assignment ID; native-session continuation is optional and may be used only when role, package, provider, model, protocol, workspace, and Git baseline identity remain verified. Provider implementations must not infer, route, or authorize review feedback.

For compatible ACP sessions, the provider supplies a Conduit-owned local stdio MCP descriptor in `session/new`. The server exposes `submit_agent_response` with a deterministic Feature 007 transport schema, maps its provider-safe flat verdict and scalar finding/global-promotion evidence fields to the canonical nullable verdict and evidence arrays, returns validation failures as tool errors within the same turn, and writes a valid canonical submission to a private ephemeral capture. The provider ignores assistant-message prose for final-response authority while this tool is active and returns only the accepted capture as `NativeTerminalResult.finalResponseCandidate`. Tool injection is process-scoped and must not mutate harness configuration.
