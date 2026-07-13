import type { CommandHandler } from "@system/bus/index.js";
import type { FeatureProvider } from "../interfaces/feature-provider.js";
import type {
  UpdateFeatureMetadataCommand,
  UpdateFeatureMetadataResult,
} from "../interfaces/commands/update-feature-metadata.js";

export function createUpdateFeatureMetadataHandler(
  provider: FeatureProvider,
): CommandHandler<UpdateFeatureMetadataCommand, UpdateFeatureMetadataResult> {
  return async (command) => {
    await provider.updateMetadata(command.featureId, {
      lifecycle: command.lifecycle,
    });
    return { success: true, data: { updated: true } };
  };
}
