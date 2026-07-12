import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type { CredentialStore } from "../interfaces/credential-store.js";
import type {
  SetCredentialCommand,
  SetCredentialResult,
} from "../interfaces/commands/set-credential.js";

export function createSetCredentialHandler(
  store: CredentialStore,
): CommandHandler<SetCredentialCommand, SetCredentialResult> {
  return async (command) => {
    await store.set(command.profile, command.key, command.value);
    return { success: true, data: { success: true } };
  };
}
