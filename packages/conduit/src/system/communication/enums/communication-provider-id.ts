export const CommunicationProviderId = {
  CodexAppServer: "codex-app-server",
  CodexExec: "codex-exec",
  OpenCodeAcp: "opencode-acp",
  OpenCodeJson: "opencode-json",
  PiRpc: "pi-rpc",
  PiJson: "pi-json",
  KiloAcp: "kilo-acp",
  KiloJson: "kilo-json",
} as const;

export type CommunicationProviderId =
  (typeof CommunicationProviderId)[keyof typeof CommunicationProviderId];
