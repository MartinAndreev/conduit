import type { Query } from "../../../../system/bus/query-bus.js";
import type { RefinementDraft } from "../../types/draft.js";

export interface ListDraftsQuery extends Query {
  readonly type: "listDrafts";
}

export interface ListDraftsReadModel {
  readonly drafts: readonly RefinementDraft[];
}
