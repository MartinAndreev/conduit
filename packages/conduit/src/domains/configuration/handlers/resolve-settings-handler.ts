import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { ConfigurationRepository } from "../repositories/configuration-repository.js";
import type {
  ResolveSettingsQuery,
  ResolveSettingsReadModel,
} from "../queries/resolve-settings.js";

export function createResolveSettingsHandler(
  configRepo: ConfigurationRepository,
): QueryHandler<ResolveSettingsQuery, ResolveSettingsReadModel> {
  return async (query) => {
    const settings = await configRepo.resolveSettings(
      query.projectRoot,
      query.cliOptions,
    );
    return { success: true, data: { settings } };
  };
}
