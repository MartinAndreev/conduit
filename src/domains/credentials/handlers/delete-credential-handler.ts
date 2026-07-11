import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { CredentialStore } from "../types/credential-store.js";
import type {
  DeleteCredentialCommand,
  DeleteCredentialResult,
} from "../commands/delete-credential.js";

export function createDeleteCredentialHandler(
  store: CredentialStore,
): CommandHandler<DeleteCredentialCommand, DeleteCredentialResult> {
  return async (command) => {
    await store.delete(command.profile, command.key);
    return { success: true, data: { success: true } };
  };
}
