import type { Query } from "../../../../system/bus/query-bus.js";
import type { RolePortrait } from "../role-portrait.js";

export interface ListPortraitsQuery extends Query {
  readonly type: "listPortraits";
}

export interface ListPortraitsReadModel {
  readonly portraits: readonly RolePortrait[];
}
