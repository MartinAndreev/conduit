import type { CommunicationProviderId } from "../enums/communication-provider-id.js";
import type { CommunicationCapabilitySnapshot } from "./provider.js";
import type { ConduitRuntimeEventType } from "./runtime-event.js";

export interface CliProviderOptions {
  readonly id: CommunicationProviderId;
  readonly runner: "codex" | "opencode" | "pi" | "kilo";
  readonly protocol: "exec-jsonl" | "run-json" | "json";
  readonly executableCandidates: readonly string[];
  readonly verifiedVersions: readonly string[];
  readonly buildArgs: (input: {
    readonly prompt: string;
    readonly model?: string;
    readonly effort?: string;
    readonly outputFile?: string;
    readonly schemaFile?: string;
  }) => readonly string[];
  readonly finalResponseCapture: CommunicationCapabilitySnapshot["finalResponseCapture"];
}

export interface ParsedNativeEvent {
  readonly type: ConduitRuntimeEventType;
  readonly payload: Readonly<Record<string, unknown>>;
  readonly nativeType?: string;
  readonly correlationId?: string;
  readonly finalResponseCandidate?: string;
  readonly nativeSessionId?: string;
}
