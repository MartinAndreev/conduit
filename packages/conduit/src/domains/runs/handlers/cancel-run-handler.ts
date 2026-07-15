import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  CancelRunCommand,
  CancelRunResult,
} from "../interfaces/commands/cancel-run.js";
import type { RunEventRepository } from "../interfaces/run-event-repository.js";
import { RunnerEventProvenance } from "../enums/runner-event-provenance.js";
import type { RunProcessRegistry } from "../repositories/run-process-registry.js";
import type { RunnerEvent } from "../types/runner-events.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";

export function createCancelRunHandler(
  eventRepository: RunEventRepository,
  processRegistry: RunProcessRegistry,
  recoveryRepository?: RunRecoveryRepository,
): CommandHandler<CancelRunCommand, CancelRunResult> {
  return async (command) => {
    try {
      // Cancel actual processes
      const cancelled = processRegistry.cancel(command.runId);

      // Append lifecycle events for each role that was running
      const entries = processRegistry.getByRun(command.runId);
      if (entries.length === 0) {
        // No active processes, just append a system-level cancel event
        const cancelEvent: RunnerEvent = {
          type: "lifecycle",
          provenance: RunnerEventProvenance.ConduitObserved,
          runId: command.runId,
          roleId: "system",
          timestamp: new Date().toISOString(),
          payload: {
            kind: "lifecycle",
            state: "cancelled",
            message: "Run cancelled by user",
          },
        };
        await eventRepository.append(cancelEvent);
      } else {
        // Append cancel event for each role
        for (const entry of entries) {
          const cancelEvent: RunnerEvent = {
            type: "lifecycle",
            provenance: RunnerEventProvenance.ConduitObserved,
            runId: command.runId,
            roleId: entry.roleId,
            timestamp: new Date().toISOString(),
            payload: {
              kind: "lifecycle",
              state: "cancelled",
              message: "Run cancelled by user",
            },
          };
          await eventRepository.append(cancelEvent);
        }
      }

      await recoveryRepository?.markCancelled(command.runId);
      return {
        success: true,
        data: {
          runId: command.runId,
          cancelled: cancelled || entries.length === 0,
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "CANCEL_RUN_ERROR",
          message: `Failed to cancel run: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
