import type { RunnerAdapter } from "./adapter.js";
import { CodexAdapter } from "./codex.js";
import { KiloAdapter } from "./kilo.js";
import { OpenCodeAdapter } from "./opencode.js";
import { PiAdapter } from "./pi.js";

const adapters: Readonly<Record<string, RunnerAdapter>> = {
  codex: new CodexAdapter(),
  opencode: new OpenCodeAdapter(),
  pi: new PiAdapter(),
  kilo: new KiloAdapter(),
};

export function configureFinalOutputCapture(
  runner: string,
  args: readonly string[],
  outputFile: string | undefined,
): readonly string[] {
  if (!outputFile) return args;
  const adapter = adapters[runner];
  return adapter?.configureFinalOutputCapture
    ? adapter.configureFinalOutputCapture(args, outputFile)
    : args;
}

export function captureFinalResponse(
  runner: string,
  runId: string,
  roleId: string,
  stdout: string,
  stderr: string,
  capturedOutput: string,
): string {
  if (capturedOutput.trim()) return capturedOutput.trim();
  const adapter = adapters[runner];
  const stdoutParser = adapter?.createOutputParser?.(runId, roleId);
  const stderrParser = adapter?.createOutputParser?.(runId, roleId);
  stdoutParser?.push(stdout);
  stdoutParser?.flush();
  stderrParser?.push(stderr);
  stderrParser?.flush();
  return (
    stdoutParser?.finalResponse ?? stderrParser?.finalResponse ?? stdout.trim()
  );
}

export function runnerAdapter(runner: string): RunnerAdapter | undefined {
  return adapters[runner];
}

export function supportedRunners(): readonly string[] {
  return Object.keys(adapters);
}
