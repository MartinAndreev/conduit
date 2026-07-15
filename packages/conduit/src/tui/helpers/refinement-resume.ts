import type { RevisionStatus } from "@domains/refinement/types/revision.js";
import type { RefinementView } from "@tui/types/refinement.js";

export function refinementResumeView(input: {
  readonly hasPacket: boolean;
  readonly hasResearch: boolean;
  readonly revisionStatus?: RevisionStatus;
}): Exclude<RefinementView, "loading" | "error"> {
  if (input.revisionStatus === "awaiting_clarification")
    return "clarifications";
  if (input.revisionStatus === "ready_for_review") return "review";
  if (
    input.hasResearch &&
    input.revisionStatus !== "approved" &&
    input.revisionStatus !== "changes_requested"
  )
    return "researchReview";
  return input.hasPacket ? "packet" : "form";
}

export function refinementApprovalRoute(input: {
  readonly architectRequested: boolean;
  readonly researchRerunRequested: boolean;
}): "save" | "research" | "architect" {
  if (!input.architectRequested) return "save";
  return input.researchRerunRequested ? "research" : "architect";
}
