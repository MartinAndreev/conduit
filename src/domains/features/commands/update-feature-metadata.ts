import type { Command } from "../../../system/bus/command-bus.js";
import type { FeatureLifecycle } from "../types/feature-provider.js";

export interface UpdateFeatureMetadataCommand extends Command {
  readonly type: "updateFeatureMetadata";
  readonly featureId: string;
  readonly lifecycle?: FeatureLifecycle;
}

export interface UpdateFeatureMetadataResult {
  readonly success: boolean;
}
