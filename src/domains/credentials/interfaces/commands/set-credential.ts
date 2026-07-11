import type { Command } from "../../../../system/bus/command-bus.js";

export interface SetCredentialCommand extends Command {
  readonly type: "setCredential";
  readonly profile: string;
  readonly key: string;
  readonly value: string;
}

export interface SetCredentialResult {
  readonly success: boolean;
}
