import { readFile } from "node:fs/promises";
import path from "node:path";
import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { FeatureProvider } from "../interfaces/feature-provider.js";
import type {
  GetFeatureContentQuery,
  FeatureContentReadModel,
} from "../interfaces/queries/get-feature-content.js";

const read = (directory: string, file: string) =>
  readFile(path.join(directory, file), "utf8").catch(() => "");
export function createGetFeatureContentHandler(
  provider: FeatureProvider,
): QueryHandler<GetFeatureContentQuery, FeatureContentReadModel> {
  return async (query) => {
    const feature = await provider.getFeature(query.featureId);
    if (!feature)
      return {
        success: false,
        error: {
          code: "FEATURE_NOT_FOUND",
          message: `Feature ${query.featureId} was not found.`,
        },
      };
    const [spec, plan, tasks, story, testCases] = await Promise.all([
      read(feature.directory, "spec.md"),
      read(feature.directory, "plan.md"),
      read(feature.directory, "tasks.md"),
      read(feature.directory, "story.md"),
      read(feature.directory, "test-cases.md"),
    ]);
    return { success: true, data: { spec, plan, tasks, story, testCases } };
  };
}
