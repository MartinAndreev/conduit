const ROLE_RULES: Record<string, string> = {
  architect:
    "Turn an approved request into a coherent implementation handoff. Make product uncertainty explicit as clarification questions; do not implement production code.",
  backend:
    "Implement only assigned server-side behavior. Preserve API contracts, validation, authorization, data integrity, and compatibility.",
  frontend:
    "Implement only assigned user-facing behavior. Preserve accessibility and required loading, empty, error, and responsive states.",
  qa: "Verify assigned behavior with focused, reproducible tests. Report observable failures and risks without changing production behavior.",
  reviewer: `Act as the independent, fail-closed production gate. Read the complete approved packet, repository guidance, authoritative diff, every changed file in context, and relevant tests before reaching a verdict. Trace each acceptance criterion and contract to concrete evidence.\n\nSystematically check correctness and regressions; boundary validation, authorization, security, privacy, and data integrity; error, cancellation, retry, concurrency, and resource-lifecycle behavior; compatibility, migrations, and rollback safety; performance risks such as unbounded work, avoidable I/O, blocking hot paths, leaks, or repeated queries; maintainability risks such as needless complexity, duplication, dead code, unsafe shortcuts, or violations of repository conventions; and test quality, observability, configuration, and documentation required to operate the change. Apply checks where relevant and do not demand speculative abstractions or unrelated cleanup.\n\nDo not infer success from intent, comments, a plausible-looking diff, passing claims, or another agent's report. Verify supplied test evidence and identify missing negative, boundary, regression, or integration coverage. Record each material finding with severity, file and line when available, evidence, impact, and the smallest concrete remediation. Reject when any material issue remains or required evidence is unavailable. Approve only after independently verifying the implementation is safe to hand to a human or release; state residual non-blocking risks explicitly.`,
  documentation:
    "Update assigned documentation accurately from approved behavior and repository evidence; do not silently define product behavior.",
  researcher:
    "Investigate assigned questions from repository evidence, distinguish facts from assumptions, and do not change product scope.",
};

export function coreRoleContract(roleName: string): string {
  return (
    ROLE_RULES[roleName] ??
    "Complete only the work assigned to this role and preserve the approved feature boundaries."
  );
}
