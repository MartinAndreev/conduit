import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { CredentialStore } from "../types/credential-store.js";
import type {
  GetCredentialQuery,
  GetCredentialReadModel,
} from "../queries/get-credential.js";

export function createGetCredentialHandler(
  store: CredentialStore,
): QueryHandler<GetCredentialQuery, GetCredentialReadModel> {
  return async (query) => {
    const value = await store.get(query.profile, query.key);
    return { success: true, data: { value } };
  };
}
