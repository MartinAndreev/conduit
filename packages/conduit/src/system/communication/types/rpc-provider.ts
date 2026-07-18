import type { CommunicationProviderId } from "../enums/communication-provider-id.js";

export type BidirectionalProtocol = "acp-stdio" | "rpc-stdio";

export interface BidirectionalProviderOptions {
  readonly id: CommunicationProviderId;
  readonly runner: "opencode" | "pi" | "kilo";
  readonly protocol: BidirectionalProtocol;
  readonly executableCandidates: readonly string[];
  readonly verifiedVersions: readonly string[];
  readonly buildArgs: (input: {
    readonly model?: string;
    readonly effort?: string;
    readonly workspaceRoot: string;
  }) => readonly string[];
}

export interface PendingRpcRequest {
  readonly resolve: (value: unknown) => void;
  readonly reject: (error: Error) => void;
}

export interface PendingPermissionRequest {
  readonly nativeRequestId: string | number;
  readonly options: readonly Readonly<Record<string, unknown>>[];
}

export interface JsonRpcResponse {
  readonly jsonrpc?: string;
  readonly id: string | number;
  readonly result?: unknown;
  readonly error?: unknown;
}
