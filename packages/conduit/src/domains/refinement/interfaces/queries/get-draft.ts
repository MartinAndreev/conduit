import type { Query } from "../../../../system/bus/query-bus.js";
import type { RefinementDraft } from "../../types/draft.js";

export interface GetDraftQuery extends Query {
  readonly type: "getDraft";
  readonly featureId: string;
}

export interface GetDraftReadModel {
  readonly draft: RefinementDraft | null;
}
