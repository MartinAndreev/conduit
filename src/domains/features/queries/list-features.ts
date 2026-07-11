import type { Query } from "../../../system/bus/query-bus.js";
import type { FeatureReadModel } from "../types/feature-provider.js";

export interface ListFeaturesQuery extends Query {
  readonly type: "listFeatures";
}

export interface ListFeaturesReadModel {
  readonly features: readonly FeatureReadModel[];
}
