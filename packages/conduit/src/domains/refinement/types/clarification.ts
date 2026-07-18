import type { ClarificationQuestion } from "./revision.js";

export interface PersistedClarificationQuestion {
  readonly id: string;
  readonly featureId: string;
  readonly revisionId: string;
  readonly fingerprint: string;
  readonly question: ClarificationQuestion;
  readonly answer?: string;
  readonly repeatCount: number;
  readonly createdAt: string;
  readonly answeredAt?: string;
}

export interface RecordClarificationResult {
  readonly unresolved: readonly PersistedClarificationQuestion[];
  readonly reminders: readonly PersistedClarificationQuestion[];
}
