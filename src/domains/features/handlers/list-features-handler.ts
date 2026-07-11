import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { FeatureProvider } from "../types/feature-provider.js";
import type {
  ListFeaturesQuery,
  ListFeaturesReadModel,
} from "../queries/list-features.js";

export function createListFeaturesHandler(
  provider: FeatureProvider,
): QueryHandler<ListFeaturesQuery, ListFeaturesReadModel> {
  return async () => {
    const features = await provider.listFeatures();
    return { success: true, data: { features } };
  };
}
