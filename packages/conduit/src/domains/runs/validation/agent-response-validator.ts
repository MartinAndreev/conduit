import { z } from "zod";
import { containsSecret } from "@system/storage/security/secret-redaction.js";
import type {
  AgentResponseV1,
  ValidationIssue,
  ValidationResult,
} from "../types/agent-protocol.js";

const maxResponseBytes = 256_000;
const pathPattern = /^(?!\/)(?!.*(?:^|\/)\.\.(?:\/|$))(?!.*\\).+/;

const boundedText = (min: number, max: number) =>
  z
    .string()
    .min(min)
    .max(max)
    .refine((value) => !containsSecret(value), {
      message: "must not contain secrets",
    });

const repoPath = boundedText(1, 300).regex(pathPattern, {
  message: "must be a normalized repository-relative path",
});

const optionalBoundedEvidence = z.array(boundedText(1, 300)).max(20).optional();

const artifactSchema = z
  .object({
    path: repoPath,
    category: z.enum([
      "source",
      "test",
      "spec",
      "contract",
      "documentation",
      "report",
      "configuration",
      "other",
    ]),
    purpose: boundedText(1, 500),
    action: z.enum(["created", "modified", "deleted", "inspected"]),
  })
  .strict();

const findingSchema = z
  .object({
    severity: z.enum(["info", "warning", "error", "critical"]),
    category: boundedText(1, 80),
    message: boundedText(1, 2000),
    path: repoPath.optional(),
    line: z.number().int().min(1).max(1_000_000).optional(),
    evidence: z.array(boundedText(1, 300)).min(1).max(20),
    suggestedRemediation: boundedText(0, 1000).optional(),
  })
  .strict();

const verificationSchema = z
  .object({
    operation: boundedText(1, 500),
    outcome: z.enum(["passed", "failed", "skipped", "blocked", "unknown"]),
    exitCode: z.number().int().min(-1).max(255).optional(),
    summary: boundedText(1, 2000),
    evidence: optionalBoundedEvidence,
  })
  .strict();

const decisionSchema = z
  .object({
    decision: boundedText(1, 500),
    rationale: boundedText(1, 2000),
    affectedPaths: z.array(repoPath).max(20).optional(),
  })
  .strict();

const blockerSchema = z
  .object({
    blocker: boundedText(1, 2000),
    impact: boundedText(1, 2000),
    minimumUnblocker: boundedText(1, 2000),
  })
  .strict();

const questionSchema = z
  .object({
    question: boundedText(1, 2000),
    whyItMatters: boundedText(1, 2000),
    context: boundedText(1, 2000),
    options: z.array(boundedText(1, 500)).min(1).max(5),
    smallestUnblocker: boundedText(1, 2000),
  })
  .strict();

const riskSchema = z
  .object({
    risk: boundedText(1, 2000),
    category: z.enum([
      "technical",
      "integration",
      "security",
      "compatibility",
      "verification",
      "operational",
      "other",
    ]),
    mitigation: boundedText(1, 2000),
  })
  .strict();

const evidenceSchema = z
  .object({
    kind: z.enum([
      "path",
      "line",
      "symbol",
      "contract",
      "command",
      "url",
      "runner_event",
      "other",
    ]),
    reference: boundedText(1, 500),
    summary: boundedText(0, 1000).optional(),
  })
  .strict();

const memoryProposalSchema = z
  .object({
    scope: z.literal("project"),
    content: boundedText(1, 2000),
    rationale: boundedText(1, 2000),
    evidence: optionalBoundedEvidence,
  })
  .strict();

const globalPromotionProposalSchema = z
  .object({
    content: boundedText(1, 2000),
    rationale: boundedText(1, 2000),
    evidence: z.array(boundedText(1, 300)).min(1).max(20),
  })
  .strict();

export const agentResponseV1Schema = z
  .object({
    protocolVersion: z.literal("1.0"),
    status: z.enum(["completed", "partial", "blocked", "needs_input", "failed"]),
    summary: boundedText(1, 2000),
    verdict: z
      .object({
        decision: z.enum([
          "approved",
          "rejected",
          "passed",
          "failed",
          "needs_changes",
          "inconclusive",
        ]),
        rationale: boundedText(1, 2000),
      })
      .strict()
      .nullable(),
    artifacts: z.array(artifactSchema).max(100),
    findings: z.array(findingSchema).max(100),
    verification: z.array(verificationSchema).max(50),
    decisions: z.array(decisionSchema).max(50),
    blockers: z.array(blockerSchema).max(50),
    questions: z.array(questionSchema).max(25),
    risks: z.array(riskSchema).max(50),
    evidence: z.array(evidenceSchema).max(100),
    memoryProposals: z.array(memoryProposalSchema).max(25),
    globalPromotionProposals: z.array(globalPromotionProposalSchema).max(25),
  })
  .strict();

function issue(path: string, message: string): ValidationIssue {
  return { path, message };
}

function issuePath(path: readonly PropertyKey[]): string {
  if (!path.length) return "$";
  return path.reduce<string>((current, part) => {
    if (typeof part === "number") return `${current}[${part}]`;
    return `${current}.${String(part)}`;
  }, "$");
}

function zodIssues(error: z.ZodError): readonly ValidationIssue[] {
  return error.issues.map((zodIssue) =>
    issue(issuePath(zodIssue.path), zodIssue.message),
  );
}

export function parseAgentResponseV1(
  raw: string,
): ValidationResult<AgentResponseV1> {
  if (Buffer.byteLength(raw, "utf8") > maxResponseBytes) {
    return {
      valid: false,
      issues: [issue("$", "response exceeds maximum size")],
    };
  }

  const trimmed = raw.trim();
  if (!trimmed.startsWith("{") || !trimmed.endsWith("}")) {
    return {
      valid: false,
      issues: [
        issue(
          "$",
          "response must be a single JSON object with no prose or Markdown fences",
        ),
      ],
    };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { valid: false, issues: [issue("$", "response is malformed JSON")] };
  }

  const result = agentResponseV1Schema.safeParse(parsed);
  return result.success
    ? { valid: true, value: result.data, issues: [] }
    : { valid: false, issues: zodIssues(result.error) };
}
