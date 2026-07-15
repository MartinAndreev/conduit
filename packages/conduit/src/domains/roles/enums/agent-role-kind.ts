export enum AgentRoleKind {
  Implementation = "implementation",
  Reviewer = "reviewer",
  Research = "research",
  Architect = "architect",
  QualityAssurance = "qa",
  Documentation = "documentation",
  Custom = "custom",
}

export type AgentRoleKindValue = `${AgentRoleKind}`;
