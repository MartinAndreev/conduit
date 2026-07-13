import type { Feature } from "@domains/features/types/feature.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
  RevisionStatus,
} from "@domains/refinement/types/revision.js";

export interface RefinementRevisionRepository {
  create(feature: Feature, feedback?: string): Promise<RefinementRevision>;
  getLatest(feature: Feature): Promise<RefinementRevision | null>;
  updateStatus(
    revision: RefinementRevision,
    status: RevisionStatus,
  ): Promise<RefinementRevision>;
  saveQuestions(
    revision: RefinementRevision,
    source: string,
  ): Promise<readonly ClarificationQuestion[]>;
  readQuestions(
    revision: RefinementRevision,
  ): Promise<readonly ClarificationQuestion[]>;
  saveAnswers(revision: RefinementRevision, answers: string): Promise<void>;
  recordReview(
    revision: RefinementRevision,
    decision: "approved" | "changes_requested",
    feedback?: string,
  ): Promise<void>;
  recordRun(revision: RefinementRevision, transcript: string): Promise<void>;
}
