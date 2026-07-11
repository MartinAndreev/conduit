import type { Command } from "../../../system/bus/command-bus.js";

export interface DeleteCredentialCommand extends Command {
  readonly type: "deleteCredential";
  readonly profile: string;
  readonly key: string;
}

export interface DeleteCredentialResult {
  readonly success: boolean;
}
