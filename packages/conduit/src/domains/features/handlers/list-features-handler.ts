import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { FeatureProvider } from "../interfaces/feature-provider.js";
import type {
  ListFeaturesQuery,
  ListFeaturesReadModel,
} from "../interfaces/queries/list-features.js";

export function createListFeaturesHandler(
  provider: FeatureProvider,
  loadImplementedFeatureIds?: () => Promise<ReadonlySet<string>>,
): QueryHandler<ListFeaturesQuery, ListFeaturesReadModel> {
  return async () => {
    const features = await provider.listFeatures();
    const implemented = await loadImplementedFeatureIds?.();
    return {
      success: true,
      data: {
        features: implemented
          ? features.map((feature) =>
              implemented.has(feature.id)
                ? {
                    ...feature,
                    metadata: { ...feature.metadata, lifecycle: "implemented" },
                  }
                : feature,
            )
          : features,
      },
    };
  };
}
