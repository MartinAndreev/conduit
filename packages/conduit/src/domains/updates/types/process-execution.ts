export interface ProcessExecutionRequest {
  readonly executable: string;
  readonly arguments: readonly string[];
  readonly cwd: string;
  readonly timeoutMs: number;
  readonly maximumOutputBytes: number;
}

export interface ProcessExecutionResult {
  readonly exitCode: number | null;
  readonly stdout: string;
  readonly stderr: string;
  readonly timedOut: boolean;
}
