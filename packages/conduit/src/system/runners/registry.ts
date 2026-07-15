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
