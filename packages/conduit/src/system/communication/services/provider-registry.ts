import os from "node:os";
import path from "node:path";
import { CommunicationProviderId } from "../enums/communication-provider-id.js";
import type { AgentCommunicationProvider } from "../types/provider.js";
import { StaticCommunicationProvider } from "../providers/static-communication-provider.js";
import { CliJsonCommunicationProvider } from "../providers/cli-json-communication-provider.js";
import { BidirectionalCommunicationProvider } from "../providers/bidirectional-communication-provider.js";

function preferred(
  input: ConstructorParameters<typeof StaticCommunicationProvider>[0],
): AgentCommunicationProvider {
  return new StaticCommunicationProvider(input);
}

export function createDefaultCommunicationProviders(): readonly AgentCommunicationProvider[] {
  const kiloCandidates = [
    "kilo",
    path.join(os.homedir(), ".kilo", "bin", "kilo"),
  ];
  return [
    preferred({
      id: CommunicationProviderId.CodexAppServer,
      runner: "codex",
      protocol: "app-server-v2",
      fallback: false,
      finalResponseCapture: "correlated-event",
    }),
    new CliJsonCommunicationProvider({
      id: CommunicationProviderId.CodexExec,
      runner: "codex",
      protocol: "exec-jsonl",
      executableCandidates: ["codex"],
      verifiedVersions: ["0.144.4"],
      finalResponseCapture: "native-final-message",
      buildArgs: ({ prompt, model, effort, outputFile, schemaFile }) => [
        "exec",
        "--json",
        "--ephemeral",
        "--ignore-rules",
        "--output-schema",
        schemaFile!,
        "--output-last-message",
        outputFile!,
        ...(model ? ["--model", model] : []),
        ...(effort
          ? ["-c", `model_reasoning_effort=${JSON.stringify(effort)}`]
          : []),
        prompt,
      ],
    }),
    new BidirectionalCommunicationProvider({
      id: CommunicationProviderId.OpenCodeAcp,
      runner: "opencode",
      protocol: "acp-stdio",
      executableCandidates: ["opencode"],
      verifiedVersions: ["1.17.18"],
      buildArgs: ({ workspaceRoot }) => [
        "acp",
        "--pure",
        "--cwd",
        workspaceRoot,
      ],
    }),
    new CliJsonCommunicationProvider({
      id: CommunicationProviderId.OpenCodeJson,
      runner: "opencode",
      protocol: "run-json",
      executableCandidates: ["opencode"],
      verifiedVersions: ["1.17.18"],
      finalResponseCapture: "json-fallback",
      buildArgs: ({ prompt, model, effort }) => [
        "run",
        "--pure",
        "--format",
        "json",
        ...(model ? ["--model", model] : []),
        ...(effort ? ["--variant", effort] : []),
        prompt,
      ],
    }),
    new BidirectionalCommunicationProvider({
      id: CommunicationProviderId.PiRpc,
      runner: "pi",
      protocol: "rpc-stdio",
      executableCandidates: ["pi"],
      verifiedVersions: ["0.80.8"],
      buildArgs: ({ model, effort }) => [
        "--mode",
        "rpc",
        "--no-extensions",
        "--no-skills",
        "--no-prompt-templates",
        "--no-context-files",
        "--no-session",
        ...(model ? ["--model", model] : []),
        ...(effort ? ["--thinking", effort] : []),
      ],
    }),
    new CliJsonCommunicationProvider({
      id: CommunicationProviderId.PiJson,
      runner: "pi",
      protocol: "json",
      executableCandidates: ["pi"],
      verifiedVersions: ["0.80.8"],
      finalResponseCapture: "json-fallback",
      buildArgs: ({ prompt, model, effort }) => [
        "--mode",
        "json",
        "--print",
        "--no-extensions",
        "--no-skills",
        "--no-prompt-templates",
        "--no-context-files",
        "--no-session",
        ...(model ? ["--model", model] : []),
        ...(effort ? ["--thinking", effort] : []),
        prompt,
      ],
    }),
    new BidirectionalCommunicationProvider({
      id: CommunicationProviderId.KiloAcp,
      runner: "kilo",
      protocol: "acp-stdio",
      executableCandidates: kiloCandidates,
      verifiedVersions: ["7.4.9"],
      buildArgs: ({ workspaceRoot }) => [
        "acp",
        "--pure",
        "--cwd",
        workspaceRoot,
      ],
    }),
    new CliJsonCommunicationProvider({
      id: CommunicationProviderId.KiloJson,
      runner: "kilo",
      protocol: "run-json",
      executableCandidates: kiloCandidates,
      verifiedVersions: ["7.4.9"],
      finalResponseCapture: "json-fallback",
      buildArgs: ({ prompt, model, effort }) => [
        "run",
        "--pure",
        "--format",
        "json",
        ...(model ? ["--model", model] : []),
        ...(effort ? ["--variant", effort] : []),
        prompt,
      ],
    }),
  ];
}

export function candidateCommunicationProviders(
  providers: readonly AgentCommunicationProvider[],
  runner: string,
): readonly AgentCommunicationProvider[] {
  return providers.filter((provider) => provider.id.startsWith(`${runner}-`));
}

export async function selectCommunicationProvider(
  providers: readonly AgentCommunicationProvider[],
  runner: string,
): Promise<{
  readonly provider: AgentCommunicationProvider;
  readonly inspection: Awaited<
    ReturnType<AgentCommunicationProvider["inspect"]>
  >;
}> {
  const candidates = candidateCommunicationProviders(providers, runner);
  const inspected = await Promise.all(
    candidates.map(async (provider) => ({
      provider,
      inspection: await provider.inspect(),
    })),
  );
  const selected = inspected.find(
    ({ inspection }) => inspection.availability.available,
  );
  if (!selected)
    throw new Error(
      inspected
        .map(({ inspection }) =>
          inspection.availability.available
            ? ""
            : inspection.availability.reason,
        )
        .filter(Boolean)
        .join("; ") || `No communication provider registered for ${runner}`,
    );
  return selected;
}
