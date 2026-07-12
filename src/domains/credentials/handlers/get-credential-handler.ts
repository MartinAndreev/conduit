import type { QueryHandler } from "../../../system/bus/query-bus.js";
import type { CredentialStore } from "../interfaces/credential-store.js";
import type {
  GetCredentialQuery,
  GetCredentialReadModel,
} from "../interfaces/queries/get-credential.js";

export function createGetCredentialHandler(
  store: CredentialStore,
): QueryHandler<GetCredentialQuery, GetCredentialReadModel> {
  return async (query) => {
    const value = await store.get(query.profile, query.key);
    return { success: true, data: { value } };
  };
}
