import type {
  RunnerEvent,
  RunnerEventPayload,
  RunnerEventType,
} from "../../domains/runs/types/runner-events.js";

export type {
  RunnerEvent,
  RunnerEventPayload,
  RunnerEventType,
} from "../../domains/runs/types/runner-events.js";

export function createEvent<T extends RunnerEventPayload>(
  type: RunnerEventType,
  runId: string,
  roleId: string,
  payload: T,
): RunnerEvent {
  return {
    type,
    runId,
    roleId,
    timestamp: new Date().toISOString(),
    payload,
  };
}
