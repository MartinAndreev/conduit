import type {
  ProcessExecutionRequest,
  ProcessExecutionResult,
} from "../types/process-execution.js";

export interface ProcessExecutor {
  execute(request: ProcessExecutionRequest): Promise<ProcessExecutionResult>;
}
