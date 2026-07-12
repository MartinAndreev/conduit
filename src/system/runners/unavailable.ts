import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";
import type { LifecyclePayload } from "../../domains/runs/types/runner-events.js";

export function createUnavailableEvent(
  runnerName: string,
  reason: string,
  runId: string,
  roleId: string,
): RunnerEvent {
  return {
    type: "lifecycle",
    runId,
    roleId,
    timestamp: new Date().toISOString(),
    payload: {
      kind: "lifecycle",
      state: "unavailable",
      message: `${runnerName}: ${reason}`,
    } satisfies LifecyclePayload,
  };
}
