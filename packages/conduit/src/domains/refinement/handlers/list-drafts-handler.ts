import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type {
  ListDraftsQuery,
  ListDraftsReadModel,
} from "../interfaces/queries/list-drafts.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export function createListDraftsHandler(
  draftRepository: DraftRepository,
): QueryHandler<ListDraftsQuery, ListDraftsReadModel> {
  return async () => {
    try {
      const drafts = await draftRepository.list();
      return {
        success: true,
        data: { drafts },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "DRAFT_LIST_ERROR",
          message: `Failed to list drafts: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
