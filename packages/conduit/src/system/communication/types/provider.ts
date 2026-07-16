import type { AgentAssignmentV1 } from "../../../domains/runs/types/agent-protocol.js";
import type { CommunicationProviderId } from "../enums/communication-provider-id.js";
import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "./runtime-event.js";

export type CommunicationProviderAvailability =
  | { readonly available: true }
  | { readonly available: false; readonly reason: string };

export interface CommunicationCapabilitySnapshot {
  readonly protocol: string;
  readonly protocolVersion?: string;
  readonly bidirectional: boolean;
  readonly sessions: boolean;
  readonly permissions: boolean;
  readonly cancellation: "native" | "process" | "unsupported";
  readonly finalResponseCapture:
    | "native-structured"
    | "native-final-message"
    | "correlated-event"
    | "jsonl-fallback"
    | "json-fallback";
  readonly telemetry: Readonly<Record<string, "supported" | "unsupported" | "unknown">>;
}

export interface CommunicationProviderInspection {
  readonly providerId: CommunicationProviderId;
  readonly runner: string;
  readonly version?: string;
  readonly availability: CommunicationProviderAvailability;
  readonly capability: CommunicationCapabilitySnapshot;
  readonly degradedReason?: string;
}

export interface PermissionResponse {
  readonly requestId: string;
  readonly decision: "approved" | "denied";
  readonly message?: string;
}

export interface CreateCommunicationSessionInput {
  readonly assignment: AgentAssignmentV1;
  readonly projectRoot: string;
  readonly workspaceRoot: string;
  readonly runner: string;
  readonly modelProvider?: string;
  readonly model?: string;
  readonly nativeSessionId?: string;
  readonly signal?: AbortSignal;
}

export interface AgentCommunicationSession {
  readonly nativeSessionId?: string;
  readonly events: AsyncGenerator<ConduitRuntimeEvent, NativeTerminalResult, void>;
  start(): Promise<void>;
  submit(assignment: AgentAssignmentV1): Promise<void>;
  respondToPermission(response: PermissionResponse): Promise<void>;
  cancel(): Promise<void>;
  close(): Promise<void>;
}

export interface AgentCommunicationProvider {
  readonly id: CommunicationProviderId;
  inspect(): Promise<CommunicationProviderInspection>;
  createSession(
    input: CreateCommunicationSessionInput,
  ): Promise<AgentCommunicationSession>;
}
