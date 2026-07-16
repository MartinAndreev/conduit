export type ConduitRuntimeEventVersion = "1.0";

export type ConduitRuntimeEventProvenance =
  | "conduit-observed"
  | "runner-reported"
  | "agent-claimed";

export type ConduitRuntimeEventType =
  | "protocol-lifecycle"
  | "agent-activity"
  | "plan-update"
  | "tool-call"
  | "command"
  | "file-operation"
  | "permission"
  | "usage"
  | "warning"
  | "native-error"
  | "dropped-events"
  | "final-response-candidate"
  | "process-outcome"
  | "worktree-change";

export type ConduitRuntimeEventPayload = Readonly<Record<string, unknown>>;

export interface ConduitRuntimeEventContext {
  readonly featureId?: string;
  readonly packageVersionId?: string;
  readonly packageHash?: string;
  readonly sessionId?: string;
  readonly turnId?: string;
  readonly runId?: string;
  readonly roleId?: string;
  readonly role?: string;
}

export interface ConduitRuntimeEventNativeMetadata {
  readonly protocol?: string;
  readonly protocolVersion?: string;
  readonly nativeType?: string;
  readonly nativeCorrelationId?: string;
}

export interface ConduitRuntimeEvent {
  readonly version: ConduitRuntimeEventVersion;
  readonly sequence: number;
  readonly receivedAt: string;
  readonly context: ConduitRuntimeEventContext;
  readonly provenance: ConduitRuntimeEventProvenance;
  readonly type: ConduitRuntimeEventType;
  readonly payload: ConduitRuntimeEventPayload;
  readonly native?: ConduitRuntimeEventNativeMetadata;
}

export type NativeTerminalStatus =
  | "completed"
  | "failed"
  | "cancelled"
  | "timed-out";

export interface NativeTerminalResult {
  readonly status: NativeTerminalStatus;
  readonly exitCode?: number;
  readonly signal?: string;
  readonly finalResponseCandidate?: string;
  readonly nativeSessionId?: string;
  readonly diagnostics: readonly string[];
}
