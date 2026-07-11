import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { CredentialStore } from "../types/credential-store.js";
import type {
  SetCredentialCommand,
  SetCredentialResult,
} from "../commands/set-credential.js";

export function createSetCredentialHandler(
  store: CredentialStore,
): CommandHandler<SetCredentialCommand, SetCredentialResult> {
  return async (command) => {
    await store.set(command.profile, command.key, command.value);
    return { success: true, data: { success: true } };
  };
}
