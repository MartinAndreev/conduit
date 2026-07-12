import type { QueryHandler } from "@system/bus/query-bus.js";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type { RefinementRevisionRepository } from "@domains/refinement/interfaces/revision-repository.js";
import type {
  GetRefinementRevisionQuery,
  GetRefinementRevisionReadModel,
} from "@domains/refinement/interfaces/queries/get-refinement-revision.js";

export function createGetRefinementRevisionHandler(deps: {
  projectRoot: string;
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  repository: RefinementRevisionRepository;
}): QueryHandler<GetRefinementRevisionQuery, GetRefinementRevisionReadModel> {
  return async (query) => {
    const config = await deps.loadConfig(deps.projectRoot);
    const feature = await deps.findFeature({
      projectRoot: deps.projectRoot,
      config,
      featureId: query.featureId,
    });
    const revision = await deps.repository.getLatest(feature);
    return {
      success: true,
      data: {
        revision,
        questions: revision
          ? await deps.repository.readQuestions(revision)
          : [],
      },
    };
  };
}
