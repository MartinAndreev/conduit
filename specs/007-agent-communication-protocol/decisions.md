# Feature 007 decisions

- The implemented `AgentAssignmentV1`, `AgentResponseV1`, and `ConduitResultRecordV1` schemas remain universal and unchanged.
- Long-term memory extends the Conduit domain behind those schemas; it does not create another handoff envelope.
- Turso is canonical state. Retrieval providers are rebuildable projections and candidate generators.
- Memvid is the required post-qualification target default and must pass an exact-version, four-target standalone gate before memory is enabled in shipped defaults.
- Hindsight is an explicitly configured daemon/HTTP alternative through the same provider-neutral port.
- Only the memory factory/registry selects a concrete provider; communication code and agents never do.
- Provider selection never falls through to another provider.
- Turso lexical retrieval is available only as explicitly configured and visibly recorded degraded mode for transient failures from a compatible selected provider; it is disabled by default.
- Existing evidence-optional agent proposals cannot autoactivate without independent evidence.
- Eligible, unambiguous project proposals may activate through deterministic Conduit policy; ambiguous cases require an explicit decision. Global promotion always requires explicit approval.
- Accepted response/proposal provenance is immutable across retries despite the existing latest-result upsert.
- Truth/evidence lifecycle, provider projection status, and embedding/representation status are independent.
- Unknown/incompatible provider versions and contract/capability/configuration/authentication failures always fail closed.
- Every role attempt receives an immutable launch-time context snapshot under a hard complete-context budget.
- Internal pack audit retains provider diagnostics; delivered Markdown contains only provider-neutral degraded/omitted notices.
- The unchanged assignment carries the generated pack through an exact transient `contextReferences` path.
- Raw transcripts, complete source files, private reasoning, secrets, and provider-generated reflections are never memory.
