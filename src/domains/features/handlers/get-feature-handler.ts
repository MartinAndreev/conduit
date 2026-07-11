import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { FeatureProvider } from "../types/feature-provider.js";
import type {
  GetFeatureQuery,
  GetFeatureReadModel,
} from "../queries/get-feature.js";

export function createGetFeatureHandler(
  provider: FeatureProvider,
): QueryHandler<GetFeatureQuery, GetFeatureReadModel> {
  return async (query) => {
    const feature = await provider.getFeature(query.featureId);
    return { success: true, data: { feature } };
  };
}
