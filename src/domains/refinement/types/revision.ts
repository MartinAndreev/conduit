export type RevisionStatus =
  | "running"
  | "awaiting_clarification"
  | "ready_for_review"
  | "approved"
  | "changes_requested"
  | "cancelled"
  | "failed";

export interface RefinementRevision {
  readonly id: string;
  readonly status: RevisionStatus;
  readonly directory: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly feedback?: string;
}

export interface ClarificationQuestion {
  readonly id: string;
  readonly question: string;
  readonly context?: string;
  readonly options: readonly string[];
}
