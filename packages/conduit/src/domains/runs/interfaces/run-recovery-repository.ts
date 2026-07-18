import type { Run } from "../types/run.js";
import type { RunSnapshot } from "../types/recovery.js";

export interface RunRecoveryRepository {
  saveSnapshot(run: Run, expectedVersion?: number): Promise<RunSnapshot>;
  claimFailedRun(
    runId: string,
    expectedVersion: number,
  ): Promise<RunSnapshot | undefined>;
  loadSnapshot(runId: string): Promise<RunSnapshot | undefined>;
  listSnapshots(limit?: number): Promise<readonly RunSnapshot[]>;
  markInterrupted(runId: string, diagnostic?: string): Promise<void>;
  markCancelled(runId: string): Promise<void>;
}
