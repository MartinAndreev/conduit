export type RunnerEventType =
  | "lifecycle"
  | "activity"
  | "tool-call"
  | "tool-output"
  | "file-change"
  | "patch"
  | "error"
  | "result";

import type { RunnerEventProvenance } from "../enums/runner-event-provenance.js";

export type LifecycleState =
  "starting" | "running" | "completed" | "failed" | "cancelled" | "unavailable";

export interface RunnerEvent {
  readonly type: RunnerEventType;
  readonly provenance: RunnerEventProvenance;
  readonly runId: string;
  readonly roleId: string;
  readonly timestamp: string;
  readonly payload: RunnerEventPayload;
}

export type RunnerEventPayload =
  | LifecyclePayload
  | ActivityPayload
  | ToolCallPayload
  | ToolOutputPayload
  | FileChangePayload
  | PatchPayload
  | ErrorPayload
  | ResultPayload;

export interface LifecyclePayload {
  readonly kind: "lifecycle";
  readonly state: LifecycleState;
  readonly message?: string;
}

export interface ActivityPayload {
  readonly kind: "activity";
  readonly message: string;
}

export interface ToolCallPayload {
  readonly kind: "tool-call";
  readonly tool: string;
  readonly args?: string;
}

export interface ToolOutputPayload {
  readonly kind: "tool-output";
  readonly tool: string;
  readonly output: string;
  readonly truncated: boolean;
}

export interface FileChangePayload {
  readonly kind: "file-change";
  readonly path: string;
  readonly additions: number;
  readonly deletions: number;
}

export interface PatchPayload {
  readonly kind: "patch";
  readonly diff: string;
  readonly fileCount: number;
}

export interface ErrorPayload {
  readonly kind: "error";
  readonly code: string;
  readonly message: string;
  readonly recoverable: boolean;
}

export interface ResultPayload {
  readonly kind: "result";
  readonly exitCode: number;
  readonly files: readonly string[];
  readonly summary: string;
}
