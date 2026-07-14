import type { Run } from "./run.js";

export type RunRecoveryState =
  "planned" | "running" | "cancelled" | "interrupted" | "complete";

export type RunSnapshot = Readonly<{
  run: Run;
  state: RunRecoveryState;
  version: number;
  updatedAt: string;
}>;
