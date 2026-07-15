# Test Cases

- Incremental JSONL parses events split across arbitrary chunks.
- Extra prose, Markdown fences, malformed JSON, multiple JSON objects, unknown properties, oversize strings, invalid enum values, invalid paths, and seeded secrets fail structural validation.
- Same structurally valid response can pass a research assignment and fail a reviewer assignment.
- `completed` implementation requires artifacts and verification; reviewer requires verdict; rejected reviewer requires findings or a rejection explanation; `needs_input` requires questions; `blocked` requires blockers.
- Process exit zero with invalid, partial, blocked, or `needs_input` final response fails semantic completion and blocks dependents.
- Research Markdown is rendered from validated universal response.
- Agents receive no database environment variables.
