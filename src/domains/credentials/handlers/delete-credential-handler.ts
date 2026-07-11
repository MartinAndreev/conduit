import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { CredentialStore } from "../interfaces/credential-store.js";
import type {
  DeleteCredentialCommand,
  DeleteCredentialResult,
} from "../interfaces/commands/delete-credential.js";

export function createDeleteCredentialHandler(
  store: CredentialStore,
): CommandHandler<DeleteCredentialCommand, DeleteCredentialResult> {
  return async (command) => {
    await store.delete(command.profile, command.key);
    return { success: true, data: { success: true } };
  };
}
