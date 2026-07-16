import type { ClarificationQuestion } from "../types/revision.js";
import type {
  PersistedClarificationQuestion,
  RecordClarificationResult,
} from "../types/clarification.js";

export interface ClarificationQuestionRepository {
  record(
    featureId: string,
    revisionId: string,
    questions: readonly ClarificationQuestion[],
  ): Promise<RecordClarificationResult>;
  answerUnresolved(
    featureId: string,
    revisionId: string,
    answer: string,
  ): Promise<void>;
  unresolved(
    featureId: string,
    revisionId: string,
  ): Promise<readonly PersistedClarificationQuestion[]>;
}
