import type { Query } from "../../../../system/bus/query-bus.js";

export interface GetFeatureContentQuery extends Query {
  readonly type: "getFeatureContent";
  readonly featureId: string;
}
export interface FeatureContentReadModel {
  readonly spec: string;
  readonly plan: string;
  readonly tasks: string;
  readonly story: string;
  readonly testCases: string;
}
