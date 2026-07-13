import type { ApplicationError } from "../../../system/bus/command-bus.js";

export const RUN_ERROR_CODES = {
  RUN_NOT_FOUND: "RUN_NOT_FOUND",
  RUN_EXECUTION_FAILED: "RUN_EXECUTION_FAILED",
  RUN_CANCELLED: "RUN_CANCELLED",
  RUNNER_UNAVAILABLE: "RUNNER_UNAVAILABLE",
  DIFF_READ_ERROR: "DIFF_READ_ERROR",
  REVIEW_SAVE_ERROR: "REVIEW_SAVE_ERROR",
  REVIEW_NOT_FOUND: "REVIEW_NOT_FOUND",
  PROCESS_NOT_FOUND: "PROCESS_NOT_FOUND",
} as const;

export type RunErrorCode =
  (typeof RUN_ERROR_CODES)[keyof typeof RUN_ERROR_CODES];

export function createRunError(
  code: RunErrorCode,
  message: string,
  cause?: unknown,
): ApplicationError {
  return { code, message, cause };
}
