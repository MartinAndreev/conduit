import { CommunicationProviderId } from "../enums/communication-provider-id.js";
import type { AgentCommunicationProvider } from "../types/provider.js";
import { StaticCommunicationProvider } from "../providers/static-communication-provider.js";

export function createDefaultCommunicationProviders(): readonly AgentCommunicationProvider[] {
  return [
    new StaticCommunicationProvider({
      id: CommunicationProviderId.CodexAppServer,
      runner: "codex",
      protocol: "app-server-v2",
      fallback: false,
      finalResponseCapture: "correlated-event",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.CodexExec,
      runner: "codex",
      protocol: "exec-jsonl",
      fallback: true,
      finalResponseCapture: "jsonl-fallback",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.OpenCodeAcp,
      runner: "opencode",
      protocol: "acp-stdio",
      fallback: false,
      finalResponseCapture: "native-final-message",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.OpenCodeJson,
      runner: "opencode",
      protocol: "run-json",
      fallback: true,
      finalResponseCapture: "json-fallback",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.PiRpc,
      runner: "pi",
      protocol: "rpc-stdio",
      fallback: false,
      finalResponseCapture: "native-final-message",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.PiJson,
      runner: "pi",
      protocol: "json",
      fallback: true,
      finalResponseCapture: "json-fallback",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.KiloAcp,
      runner: "kilo",
      protocol: "acp-stdio",
      fallback: false,
      finalResponseCapture: "native-final-message",
    }),
    new StaticCommunicationProvider({
      id: CommunicationProviderId.KiloJson,
      runner: "kilo",
      protocol: "run-json",
      fallback: true,
      finalResponseCapture: "json-fallback",
    }),
  ];
}

export function candidateCommunicationProviders(
  providers: readonly AgentCommunicationProvider[],
  runner: string,
): readonly AgentCommunicationProvider[] {
  return providers.filter((provider) => provider.id.startsWith(`${runner}-`));
}
