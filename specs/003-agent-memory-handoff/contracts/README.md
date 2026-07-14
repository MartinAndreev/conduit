# Contracts

Feature 003 contracts to define during implementation:

- `MemoryRepository` for nodes, versions, lifecycle states, evidence, relations, and audit history.
- `MemoryProposalRepository` and proposal validation contracts.
- `SourceIndexRepository` and deterministic indexer contracts.
- `EvidenceResolver` and stale-memory invalidation contracts.
- `EmbeddingProvider` port and embedding repository contracts.
- `MemoryRetriever` contract for query construction, eligibility, candidate generation, rank fusion, and score explanation.
- `ContextPackCompiler` and `ContextPackRepository` contracts.
- Versioned `AgentHandoffEnvelope` contract.
- `HandoffRepository` and handoff validation/redaction contracts.
- `MemoryPromotionRepository` and explicit global-promotion approval contracts.
- `RetrievalAttributionRepository` and feedback contracts.

Contracts must follow repository domain conventions: command/query interfaces in the owning domain, repository contracts in domain `interfaces`, value types in domain `types`, enums in domain `enums`, and errors in domain `errors`.
