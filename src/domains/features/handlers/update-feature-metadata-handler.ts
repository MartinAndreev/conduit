import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { FeatureProvider } from "../types/feature-provider.js";
import type {
  UpdateFeatureMetadataCommand,
  UpdateFeatureMetadataResult,
} from "../commands/update-feature-metadata.js";

export function createUpdateFeatureMetadataHandler(
  provider: FeatureProvider,
): CommandHandler<UpdateFeatureMetadataCommand, UpdateFeatureMetadataResult> {
  return async (command) => {
    await provider.updateMetadata(command.featureId, {
      lifecycle: command.lifecycle,
    });
    return { success: true, data: { success: true } };
  };
}
