import type { Query } from "@system/bus/query-bus.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
} from "@domains/refinement/types/revision.js";

export interface GetRefinementRevisionQuery extends Query {
  readonly type: "getRefinementRevision";
  readonly featureId: string;
}

export interface GetRefinementRevisionReadModel {
  readonly revision: RefinementRevision | null;
  readonly questions: readonly ClarificationQuestion[];
}
