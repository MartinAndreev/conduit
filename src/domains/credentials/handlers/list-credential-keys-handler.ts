import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { CredentialStore } from "../interfaces/credential-store.js";
import type {
  ListCredentialKeysQuery,
  ListCredentialKeysReadModel,
} from "../interfaces/queries/list-credential-keys.js";

export function createListCredentialKeysHandler(
  store: CredentialStore,
): QueryHandler<ListCredentialKeysQuery, ListCredentialKeysReadModel> {
  return async (query) => {
    const keys = await store.list(query.profile);
    return { success: true, data: { keys } };
  };
}
