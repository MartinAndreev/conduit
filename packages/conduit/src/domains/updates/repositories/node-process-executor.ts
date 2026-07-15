import { spawn } from "node:child_process";
import type { ProcessExecutor } from "../interfaces/process-executor.js";
import type {
  ProcessExecutionRequest,
  ProcessExecutionResult,
} from "../types/process-execution.js";

const ALLOWED_ENVIRONMENT_KEYS = [
  "PATH",
  "HOME",
  "USERPROFILE",
  "APPDATA",
  "LOCALAPPDATA",
  "TMPDIR",
  "TEMP",
  "TMP",
  "XDG_CONFIG_HOME",
  "XDG_DATA_HOME",
  "SystemRoot",
  "WINDIR",
  "COMSPEC",
  "PATHEXT",
] as const;

function minimalEnvironment(): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = {};
  for (const key of ALLOWED_ENVIRONMENT_KEYS) {
    const value = process.env[key];
    if (value !== undefined) environment[key] = value;
  }
  return environment;
}

function boundedAppend(
  current: Buffer<ArrayBufferLike>,
  chunk: Buffer<ArrayBufferLike>,
  maximum: number,
): Buffer<ArrayBufferLike> {
  if (current.byteLength >= maximum) return current;
  return Buffer.concat([
    current,
    chunk.subarray(0, maximum - current.byteLength),
  ]);
}

export class NodeProcessExecutor implements ProcessExecutor {
  execute(request: ProcessExecutionRequest): Promise<ProcessExecutionResult> {
    return new Promise((resolve, reject) => {
      const child = spawn(request.executable, [...request.arguments], {
        cwd: request.cwd,
        shell: false,
        windowsHide: true,
        stdio: ["ignore", "pipe", "pipe"],
        env: minimalEnvironment(),
      });
      let stdout: Buffer<ArrayBufferLike> = Buffer.alloc(0);
      let stderr: Buffer<ArrayBufferLike> = Buffer.alloc(0);
      let timedOut = false;
      const timeout = setTimeout(() => {
        timedOut = true;
        child.kill("SIGTERM");
      }, request.timeoutMs);
      child.stdout.on("data", (chunk: Buffer<ArrayBufferLike>) => {
        stdout = boundedAppend(stdout, chunk, request.maximumOutputBytes);
      });
      child.stderr.on("data", (chunk: Buffer<ArrayBufferLike>) => {
        stderr = boundedAppend(stderr, chunk, request.maximumOutputBytes);
      });
      child.once("error", (error) => {
        clearTimeout(timeout);
        reject(error);
      });
      child.once("close", (exitCode) => {
        clearTimeout(timeout);
        resolve({
          exitCode,
          stdout: stdout.toString("utf8"),
          stderr: stderr.toString("utf8"),
          timedOut,
        });
      });
    });
  }
}
