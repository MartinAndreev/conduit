import type { AgentAssignmentV1 } from "../../../domains/runs/types/agent-protocol.js";
import type { CommunicationProviderId } from "../enums/communication-provider-id.js";
import type {
  AgentCommunicationProvider,
  AgentCommunicationSession,
  CommunicationCapabilitySnapshot,
  CommunicationProviderInspection,
  CreateCommunicationSessionInput,
  PermissionResponse,
} from "../types/provider.js";
import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "../types/runtime-event.js";

export interface StaticCommunicationProviderOptions {
  readonly id: CommunicationProviderId;
  readonly runner: string;
  readonly protocol: string;
  readonly fallback: boolean;
  readonly finalResponseCapture: CommunicationCapabilitySnapshot["finalResponseCapture"];
}

export class StaticCommunicationProvider implements AgentCommunicationProvider {
  readonly id: CommunicationProviderId;
  private readonly options: StaticCommunicationProviderOptions;

  constructor(options: StaticCommunicationProviderOptions) {
    this.id = options.id;
    this.options = options;
  }

  async inspect(): Promise<CommunicationProviderInspection> {
    return {
      providerId: this.id,
      runner: this.options.runner,
      availability: { available: false, reason: "provider not negotiated" },
      degradedReason: this.options.fallback ? "verified fallback provider" : undefined,
      capability: {
        protocol: this.options.protocol,
        bidirectional: !this.options.fallback,
        sessions: !this.options.fallback,
        permissions: !this.options.fallback,
        cancellation: this.options.fallback ? "process" : "native",
        finalResponseCapture: this.options.finalResponseCapture,
        telemetry: {
          lifecycle: "supported",
          activity: "supported",
          tools: "unknown",
          commands: "unknown",
          files: "unknown",
          usage: "unknown",
        },
      },
    };
  }

  async createSession(
    input: CreateCommunicationSessionInput,
  ): Promise<AgentCommunicationSession> {
    return new UnstartedCommunicationSession(input.nativeSessionId);
  }
}

class UnstartedCommunicationSession implements AgentCommunicationSession {
  readonly nativeSessionId?: string;
  readonly events: AsyncGenerator<ConduitRuntimeEvent, NativeTerminalResult, void>;

  constructor(nativeSessionId: string | undefined) {
    this.nativeSessionId = nativeSessionId;
    this.events = this.createEvents();
  }

  async start(): Promise<void> {}

  async submit(_assignment: AgentAssignmentV1): Promise<void> {}

  async respondToPermission(_response: PermissionResponse): Promise<void> {}

  async cancel(): Promise<void> {}

  async close(): Promise<void> {
    await this.events.return?.({
      status: "cancelled",
      nativeSessionId: this.nativeSessionId,
      diagnostics: ["session closed before provider launch"],
    });
  }

  private async *createEvents(): AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  > {
    yield {
      version: "1.0",
      sequence: 1,
      receivedAt: new Date().toISOString(),
      context: {},
      provenance: "conduit-observed",
      type: "native-error",
      payload: {
        code: "PROVIDER_NOT_IMPLEMENTED",
        message: "Provider scaffold has no launched transport.",
      },
    };
    return {
      status: "failed",
      nativeSessionId: this.nativeSessionId,
      diagnostics: ["provider scaffold has no launched transport"],
    };
  }
}
