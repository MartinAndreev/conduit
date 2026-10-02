---
name: duplication-audit
description: Analyze Conduit Go code for duplicated decisions, repeated mechanisms, and scattered access to shared state or resources that require coordinated edits. Recommend ownership and reuse boundaries with concrete evidence; do not automatically refactor or merge superficially similar code.
---

# Duplication and shared-access audit

Identify where one logical change requires coordinated edits and where multiple access paths can enforce inconsistent guarantees. This is an advisory analysis, not an implementation task or acceptance gate. Use the language of the current conversation unless another output language is explicitly requested. If delegated, pass the conversation language to the reviewer.

## Scope and evidence

- Read `AGENTS.md` and its architecture/verification instructions, then the applicable feature contracts. Use the project's definitions of rule, query, step, procedure, wrapper, and skeleton rather than duplicating their policy here.
- Establish the requested feature, package, or repository scope and current snapshot. Follow relevant callers beyond a diff; state where exploration stops. Ask about materially unclear scope, semantics, or ownership instead of inventing a unification policy.
- Search with `rg` first. Inspect candidates and their callers manually. Clone detectors, if already available and useful, supply candidates only; matching text or a duplication percentage is not proof of a design problem. Do not install tooling or introduce dependencies merely to produce a score.
- Read-only analysis: do not change code, tests, architecture contracts, or commits. Do not execute live harness/model calls or access production databases to investigate a candidate. Separate suggested work from authorized implementation.

## What to examine

1. **Repeated knowledge:** the same business decision, invariant, revision transition, validation, or error policy implemented in more than one place, including differently written implementations.
2. **Repeated mechanisms:** lock scopes, transaction boundaries, lifecycle cleanup, retries, event construction, and transport error mapping. Check whether an existing abstraction is bypassed before proposing another one.
3. **Shared state/resource access:** map relevant reads and writes, entry points, and guards for a shared state field, repository, connection, process, transport, or event stream. Compare paths for lock, transaction, validation, correlation, and error guarantees. Multiple callers are not inherently wrong; identify conflicting or duplicated policy and uncontrolled bypasses.
4. **Repeated flows:** success, failure, and reconciliation paths whose ordering and guarantees are genuinely shared. Distinguish a common skeleton from provider-specific behavior.
5. **Existing abstraction costs:** inspect whether a shared helper already mixes unrelated owners, hides effects, or requires flags/type switches for incompatible semantics. Sometimes keeping behavior separate, or narrowing an existing abstraction, is preferable to more reuse.

For each candidate, answer: what future change would force edits at several sites; which sites are independently maintained; what inconsistency can result if one is missed? Separate potential inconsistency from an already demonstrated defect.

## Decide whether reuse is justified

- Compare meaning, ownership, preconditions, results, effects, sequencing, concurrency, failure, and retry behavior. Explain differences before suggesting consolidation. Do not silently resolve contradictory policies.
- Recommend the smallest appropriate owner and abstraction: business knowledge in its domain rule, a single effect in a step, sequencing in a procedure, a repeated technical mechanism in a wrapper, and proven shared ordering in a skeleton. Access to concrete resources belongs behind the relevant port/adapter/repository boundary.
- Show how each existing caller would use the proposed boundary and which differences remain local. Do not create a catch-all utility/service solely to reduce the number of repeated lines.
- Preserve atomic read/check/mutate sequences. Several individually locked helpers do not replace one atomic critical section. Reuse must not move I/O under a session mutex, obscure transaction scope, swallow failures, or introduce automatic retries.
- Evaluate human readability: will a developer be able to locate the authoritative policy and follow the operation with fewer conceptual jumps? Fewer lines alone are not sufficient.

## Report

Start with the highest-impact places requiring coordinated changes, not a catalogue of every similar fragment. For each meaningful finding provide:

- Linked files/symbols and evidence of the repeated knowledge or access paths.
- The common rule/resource/mechanism and observed behavioral differences.
- A concrete change scenario, affected sites, and consequence of divergence.
- Whether it is an evidenced defect, improvement opportunity, intentional variation, or unresolved question; prioritize by consequence rather than repetition count.
- The recommended owner and smallest reuse boundary, or a reason to keep the implementations separate.
- Compatibility/concurrency risks and focused behavior checks needed before any refactor. Clearly mark architectural choices requiring user agreement.

Use a compact access-path table or Mermaid diagram when it makes ownership or bypasses easier to see. State scope, snapshot, checks actually performed, uncertainty, and unexplored areas. Do not claim exhaustive coverage from a search alone.

Return recommendations in the task unless a saved report is requested with an agreed feature-document destination. Do not fix or block on findings. This skill complements `implementation-audit`: load it when reuse or scattered-access analysis is requested or warranted by concrete findings, not automatically for every review. Feed relevant confirmed hotspots into a walkthrough when one is being prepared, without duplicating entire reports.
