import type { AgentAssignmentPolicyV1, AgentResponseV1, ValidationIssue, ValidationResult } from "../types/agent-protocol.js";

function issue(path: string, message: string): ValidationIssue { return { path, message }; }
function overlaps(path: string, owned: readonly string[]): boolean { return owned.length === 0 || owned.some((root) => path === root || path.startsWith(`${root}/`) || root === "."); }

export function validateAgentResponseForAssignment(response: AgentResponseV1, policy: AgentAssignmentPolicyV1): ValidationResult<AgentResponseV1> {
  const issues: ValidationIssue[] = [];
  if (response.status === "needs_input" && response.questions.length === 0) issues.push(issue("$.questions", "needs_input requires at least one question"));
  if (response.status === "blocked" && response.blockers.length === 0) issues.push(issue("$.blockers", "blocked requires at least one blocker"));
  if (response.status === "completed") {
    if ((policy.roleKind === "implementation" || policy.roleKind === "documentation" || policy.roleKind === "architect") && response.artifacts.length === 0) issues.push(issue("$.artifacts", `${policy.roleKind} completion requires artifacts`));
    if ((policy.roleKind === "implementation" || policy.roleKind === "qa") && response.verification.length === 0) issues.push(issue("$.verification", `${policy.roleKind} completion requires verification`));
    if (policy.roleKind === "reviewer" && !response.verdict) issues.push(issue("$.verdict", "reviewer completion requires a verdict"));
    if (policy.roleKind === "research" && response.findings.some((finding) => finding.evidence.length === 0)) issues.push(issue("$.findings", "research findings require evidence"));
  }
  if (response.verdict?.decision === "rejected" && response.findings.length === 0 && response.verdict.rationale.trim().length < 10) issues.push(issue("$.verdict", "rejected verdict requires material findings or explicit rationale"));
  for (const artifact of response.artifacts) if (artifact.action !== "inspected" && !overlaps(artifact.path, policy.ownedPaths)) issues.push(issue("$.artifacts.path", `reported modification outside owned paths: ${artifact.path}`));
  for (const required of policy.requiredVerification ?? []) if (response.status === "completed" && !response.verification.some((v) => v.operation.includes(required))) issues.push(issue("$.verification", `required verification omitted: ${required}`));
  return issues.length ? { valid: false, value: response, issues } : { valid: true, value: response, issues: [] };
}

export function roleKindForRole(roleName: string): AgentAssignmentPolicyV1["roleKind"] {
  if (roleName === "reviewer") return "reviewer";
  if (roleName === "researcher" || roleName === "research") return "research";
  if (roleName === "architect") return "architect";
  if (roleName === "qa") return "qa";
  if (roleName === "documentation" || roleName === "docs") return "documentation";
  if (roleName === "frontend" || roleName === "backend" || roleName.includes("implementation")) return "implementation";
  return "custom";
}
