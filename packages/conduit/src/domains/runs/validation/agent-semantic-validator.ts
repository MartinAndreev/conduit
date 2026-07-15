import type {
  AgentAssignmentPolicyV1,
  AgentResponseV1,
  ValidationIssue,
  ValidationResult,
} from "../types/agent-protocol.js";
import { AgentRoleKind } from "../../roles/enums/agent-role-kind.js";

export const defaultRoleKinds: Readonly<
  Record<string, AgentAssignmentPolicyV1["roleKind"]>
> = {
  reviewer: AgentRoleKind.Reviewer,
  researcher: AgentRoleKind.Research,
  research: AgentRoleKind.Research,
  architect: AgentRoleKind.Architect,
  qa: AgentRoleKind.QualityAssurance,
  documentation: AgentRoleKind.Documentation,
  docs: AgentRoleKind.Documentation,
  frontend: AgentRoleKind.Implementation,
  backend: AgentRoleKind.Implementation,
};

function issue(path: string, message: string): ValidationIssue {
  return { path, message };
}

function overlaps(path: string, ownedPaths: readonly string[]): boolean {
  return ownedPaths.some(
    (root) => path === root || path.startsWith(`${root}/`) || root === ".",
  );
}

function requiresChangedArtifacts(
  roleKind: AgentAssignmentPolicyV1["roleKind"],
): boolean {
  return (
    roleKind === AgentRoleKind.Implementation ||
    roleKind === AgentRoleKind.Documentation ||
    roleKind === AgentRoleKind.Architect
  );
}

function allowsFailedVerificationEvidence(
  roleKind: AgentAssignmentPolicyV1["roleKind"],
): boolean {
  return (
    roleKind === AgentRoleKind.Research ||
    roleKind === AgentRoleKind.QualityAssurance ||
    roleKind === AgentRoleKind.Reviewer
  );
}

function validateStatusSections(
  response: AgentResponseV1,
  policy: AgentAssignmentPolicyV1,
  issues: ValidationIssue[],
): void {
  if (response.status === "needs_input" && response.questions.length === 0) {
    issues.push(
      issue("$.questions", "needs_input requires at least one question"),
    );
  }
  if (response.status === "blocked" && response.blockers.length === 0) {
    issues.push(issue("$.blockers", "blocked requires at least one blocker"));
  }
  if (response.status !== "completed") {
    return;
  }

  if (requiresChangedArtifacts(policy.roleKind) && !response.artifacts.length) {
    issues.push(
      issue("$.artifacts", `${policy.roleKind} completion requires artifacts`),
    );
  }
  if (
    (policy.roleKind === AgentRoleKind.Implementation ||
      policy.roleKind === AgentRoleKind.QualityAssurance) &&
    !response.verification.length
  ) {
    issues.push(
      issue(
        "$.verification",
        `${policy.roleKind} completion requires verification`,
      ),
    );
  }
  if (
    response.verification.some(
      (item) =>
        item.outcome !== "passed" &&
        (item.outcome !== "failed" ||
          !allowsFailedVerificationEvidence(policy.roleKind)),
    )
  ) {
    issues.push(
      issue(
        "$.verification",
        allowsFailedVerificationEvidence(policy.roleKind)
          ? "completed evaluation requires every reported verification outcome to be passed or failed; skipped, blocked, and unknown outcomes are incomplete"
          : "completed status requires every reported verification outcome to be passed",
      ),
    );
  }
  if (policy.roleKind === AgentRoleKind.Reviewer && !response.verdict) {
    issues.push(issue("$.verdict", "reviewer completion requires a verdict"));
  }
}

function validateArtifactClaims(
  response: AgentResponseV1,
  policy: AgentAssignmentPolicyV1,
  issues: ValidationIssue[],
): void {
  for (const artifact of response.artifacts) {
    if (
      artifact.action !== "inspected" &&
      !overlaps(artifact.path, policy.ownedPaths)
    ) {
      issues.push(
        issue(
          "$.artifacts.path",
          `reported modification outside owned paths: ${artifact.path}`,
        ),
      );
    }
  }

  const observed = policy.observedChangedFiles ?? [];
  if (policy.readOnly && observed.length > 0) {
    issues.push(
      issue(
        "$.artifacts",
        `read-only assignment changed files: ${observed.join(", ")}`,
      ),
    );
  }
  for (const changedFile of observed) {
    if (!overlaps(changedFile, policy.ownedPaths)) {
      issues.push(
        issue(
          "$.artifacts",
          `Conduit observed a change outside owned paths: ${changedFile}`,
        ),
      );
    }
  }

  const claimsObservedChange = response.artifacts.some(
    (artifact) =>
      artifact.action !== "inspected" &&
      observed.some(
        (changedFile) =>
          changedFile === artifact.path ||
          changedFile.startsWith(`${artifact.path}/`),
      ),
  );
  if (
    response.status === "completed" &&
    requiresChangedArtifacts(policy.roleKind) &&
    observed.length > 0 &&
    !claimsObservedChange
  ) {
    issues.push(
      issue(
        "$.artifacts",
        "reported artifacts do not match Conduit-observed changed files",
      ),
    );
  }
}

function validateAssignmentRequirements(
  response: AgentResponseV1,
  policy: AgentAssignmentPolicyV1,
  issues: ValidationIssue[],
): void {
  if (response.status !== "completed") {
    return;
  }
  for (const required of policy.requiredVerification ?? []) {
    if (
      !response.verification.some((item) => item.operation.includes(required))
    ) {
      issues.push(
        issue("$.verification", `required verification omitted: ${required}`),
      );
    }
  }
  for (const expected of policy.expectedArtifacts ?? []) {
    if (
      !response.artifacts.some(
        (artifact) =>
          artifact.path === expected ||
          artifact.path.startsWith(`${expected}/`),
      )
    ) {
      issues.push(
        issue("$.artifacts", `expected artifact omitted: ${expected}`),
      );
    }
  }
}

export function validateAgentResponseForAssignment(
  response: AgentResponseV1,
  policy: AgentAssignmentPolicyV1,
): ValidationResult<AgentResponseV1> {
  const issues: ValidationIssue[] = [];
  validateStatusSections(response, policy, issues);
  validateArtifactClaims(response, policy, issues);
  validateAssignmentRequirements(response, policy, issues);

  if (
    response.verdict?.decision === "rejected" &&
    response.findings.length === 0 &&
    response.verdict.rationale.trim().length < 10
  ) {
    issues.push(
      issue(
        "$.verdict",
        "rejected verdict requires material findings or explicit rationale",
      ),
    );
  }

  return issues.length
    ? { valid: false, value: response, issues }
    : { valid: true, value: response, issues: [] };
}

export function roleKindForRole(
  roleName: string,
  configuredRoleKind?: AgentAssignmentPolicyV1["roleKind"],
): AgentAssignmentPolicyV1["roleKind"] {
  return (
    configuredRoleKind ?? defaultRoleKinds[roleName] ?? AgentRoleKind.Custom
  );
}
