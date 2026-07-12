import type { Query } from "../../../../system/bus/query-bus.js";

export interface ListCredentialKeysQuery extends Query {
  readonly type: "listCredentialKeys";
  readonly profile: string;
}

export interface ListCredentialKeysReadModel {
  readonly keys: readonly string[];
}
