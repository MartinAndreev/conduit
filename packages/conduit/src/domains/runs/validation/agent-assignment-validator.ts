import { z } from "zod";
import type {
  AgentAssignmentV1,
  ValidationIssue,
  ValidationResult,
} from "../types/agent-protocol.js";
import { AgentRoleKind } from "../../roles/enums/agent-role-kind.js";

const repositoryPath = z
  .string()
  .min(1)
  .max(300)
  .regex(/^(?!\/)(?!.*(?:^|\/)\.\.(?:\/|$))(?!.*\\).+/, {
    message: "must be a normalized repository-relative path",
  });

const boundedItems = (maximum: number) =>
  z.array(z.string().min(1).max(500)).max(maximum);

export const agentAssignmentV1Schema = z
  .object({
    assignmentId: z.string().min(1).max(120),
    role: z.string().min(1).max(80),
    roleKind: z.nativeEnum(AgentRoleKind),
    objective: z.string().min(1).max(4_000),
    ownedPaths: z.array(repositoryPath).max(100),
    forbiddenPaths: z.array(repositoryPath).max(100),
    dependencies: boundedItems(50),
    contextReferences: z.array(repositoryPath).max(100),
    acceptanceCriteria: boundedItems(100),
    contracts: z.array(repositoryPath).max(100),
    requiredVerification: boundedItems(50),
    expectedCapabilities: boundedItems(50),
    requiredResponseFields: z
      .array(
        z.enum([
          "protocolVersion",
          "status",
          "summary",
          "verdict",
          "artifacts",
          "findings",
          "verification",
          "decisions",
          "blockers",
          "questions",
          "risks",
          "evidence",
          "memoryProposals",
          "globalPromotionProposals",
        ]),
      )
      .length(14),
    outputSchemaRef: z.literal("agent-response-v1.schema.json"),
    security: z
      .object({
        databaseEnvironmentRemoved: z.literal(true),
        databaseAccessForbidden: z.literal(true),
        memoryActivationForbidden: z.literal(true),
        secretReportingForbidden: z.literal(true),
      })
      .strict(),
    sizeLimits: z
      .object({
        responseBytes: z.number().int().positive().max(256_000),
        summaryCharacters: z.number().int().positive().max(2_000),
        collectionItems: z.number().int().positive().max(100),
      })
      .strict(),
  })
  .strict();

export function validateAgentAssignmentV1(
  assignment: unknown,
): ValidationResult<AgentAssignmentV1> {
  const result = agentAssignmentV1Schema.safeParse(assignment);
  if (result.success) {
    return { valid: true, value: result.data, issues: [] };
  }
  const issues: ValidationIssue[] = result.error.issues.map((item) => ({
    path: item.path.length ? `$.${item.path.join(".")}` : "$",
    message: item.message,
  }));
  return { valid: false, issues };
}
