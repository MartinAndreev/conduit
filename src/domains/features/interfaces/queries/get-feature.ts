import type { Query } from "../../../../system/bus/query-bus.js";
import type { FeatureReadModel } from "../../types/feature.js";

export interface GetFeatureQuery extends Query {
  readonly type: "getFeature";
  readonly featureId: string;
}

export interface GetFeatureReadModel {
  readonly feature: FeatureReadModel | undefined;
}
