import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";

export type RunnerAvailability =
  | { readonly available: true }
  | { readonly available: false; readonly reason: string };

export interface RunnerAdapter {
  readonly name: string;
  readonly command: string;
  checkAvailability(): Promise<RunnerAvailability>;
  buildArgs(promptFile: string, model?: string): readonly string[];
  configureFinalOutputCapture?(
    args: readonly string[],
    outputFile: string,
  ): readonly string[];
  parseOutput(
    raw: string,
    runId: string,
    roleId: string,
  ): readonly RunnerEvent[];
}
