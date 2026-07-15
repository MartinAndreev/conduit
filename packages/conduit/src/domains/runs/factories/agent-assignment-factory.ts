import type {
  AgentAssignmentV1,
  AgentRoleKind,
} from "../types/agent-protocol.js";

export const requiredAgentResponseFields = [
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
] as const;

function normalizeRepositoryPath(repositoryPath: string): string {
  if (/^(\.\/)+$/.test(repositoryPath)) return ".";
  const withoutPrefix = repositoryPath.replace(/^(\.\/)+/, "");
  const withoutTrailingSlash = withoutPrefix.replace(/\/+$/, "");
  return withoutTrailingSlash || repositoryPath;
}

export function createAgentAssignmentV1(input: {
  assignmentId: string;
  role: string;
  roleKind: AgentRoleKind;
  objective: string;
  ownedPaths: readonly string[];
  forbiddenPaths?: readonly string[];
  dependencies?: readonly string[];
  contextReferences: readonly string[];
  acceptanceCriteria: readonly string[];
  contracts: readonly string[];
  requiredVerification?: readonly string[];
  expectedCapabilities?: readonly string[];
}): AgentAssignmentV1 {
  return {
    assignmentId: input.assignmentId,
    role: input.role,
    roleKind: input.roleKind,
    objective: input.objective,
    ownedPaths: input.ownedPaths.map(normalizeRepositoryPath),
    forbiddenPaths: (input.forbiddenPaths ?? []).map(normalizeRepositoryPath),
    dependencies: input.dependencies ?? [],
    contextReferences: input.contextReferences,
    acceptanceCriteria: input.acceptanceCriteria,
    contracts: input.contracts,
    requiredVerification: input.requiredVerification ?? [],
    expectedCapabilities: input.expectedCapabilities ?? [],
    requiredResponseFields: requiredAgentResponseFields,
    outputSchemaRef: "agent-response-v1.schema.json",
    security: {
      databaseEnvironmentRemoved: true,
      databaseAccessForbidden: true,
      memoryActivationForbidden: true,
      secretReportingForbidden: true,
    },
    sizeLimits: {
      responseBytes: 256_000,
      summaryCharacters: 2_000,
      collectionItems: 100,
    },
  };
}
