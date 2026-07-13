import type { Query } from "../../../system/bus/query-bus.js";
import type { ResolvedSettings } from "../types/settings.js";

export interface ResolveSettingsQuery extends Query {
  readonly type: "resolveSettings";
  readonly projectRoot: string;
  readonly cliOptions?: Record<string, unknown>;
}

export interface ResolveSettingsReadModel {
  readonly settings: ResolvedSettings;
}
