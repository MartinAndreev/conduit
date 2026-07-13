import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  GetDraftQuery,
  GetDraftReadModel,
} from "../interfaces/queries/get-draft.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export function createGetDraftHandler(
  draftRepository: DraftRepository,
): QueryHandler<GetDraftQuery, GetDraftReadModel> {
  return async (query) => {
    try {
      const draft = await draftRepository.load(query.featureId);
      return {
        success: true,
        data: { draft },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DRAFT_LOAD_ERROR",
          message: `Failed to load draft: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
