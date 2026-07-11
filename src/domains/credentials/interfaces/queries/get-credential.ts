import type { Query } from "../../../../system/bus/query-bus.js";

export interface GetCredentialQuery extends Query {
  readonly type: "getCredential";
  readonly profile: string;
  readonly key: string;
}

export interface GetCredentialReadModel {
  readonly value: string | undefined;
}
