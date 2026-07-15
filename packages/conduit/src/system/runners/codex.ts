import { spawnSync } from "node:child_process";
import type { RunnerAdapter, RunnerAvailability } from "./adapter.js";
import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";
import { createEvent } from "./events.js";
import { JsonLineOutputParser } from "./jsonl-parser.js";

export class CodexAdapter implements RunnerAdapter {
  readonly name = "codex";
  readonly command = "codex";

  async checkAvailability(): Promise<RunnerAvailability> {
    const result = spawnSync("which", ["codex"], { encoding: "utf8" });
    if (result.status !== 0) {
      return { available: false, reason: "codex executable not found in PATH" };
    }
    return { available: true };
  }

  buildArgs(promptFile: string, model?: string): readonly string[] {
    const args = ["exec"];
    if (model) args.push("--model", model);
    args.push(`Read ${promptFile} and perform only your assigned task.`);
    return args;
  }

  configureFinalOutputCapture(
    args: readonly string[],
    outputFile: string,
  ): readonly string[] {
    const promptArgument = args.at(-1);
    if (!promptArgument)
      throw new Error("Could not configure Codex final-output capture.");
    return [
      ...args.slice(0, -1),
      "--output-last-message",
      outputFile,
      promptArgument,
    ];
  }


  createOutputParser(runId: string, roleId: string): JsonLineOutputParser {
    return new JsonLineOutputParser(
      (line) => this.parseOutput(`${line}\n`, runId, roleId),
      (line) =>
        createEvent("activity", runId, roleId, {
          kind: "activity",
          message: line.slice(0, 200),
        }),
    );
  }

  parseOutput(
    raw: string,
    runId: string,
    roleId: string,
  ): readonly RunnerEvent[] {
    const events: RunnerEvent[] = [];
    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const parsed = JSON.parse(trimmed);
        events.push(...this.parseJsonl(parsed, runId, roleId));
      } catch {
        events.push(
          createEvent("activity", runId, roleId, {
            kind: "activity",
            message: trimmed.slice(0, 200),
          }),
        );
      }
    }
    return events;
  }

  private parseJsonl(
    parsed: Record<string, unknown>,
    runId: string,
    roleId: string,
  ): RunnerEvent[] {
    const events: RunnerEvent[] = [];
    const type = parsed.type as string | undefined;
    if (type === "message" && parsed.role === "assistant") {
      const content = parsed.content;
      if (typeof content === "string") {
        events.push(
          createEvent("activity", runId, roleId, {
            kind: "activity",
            message: content.slice(0, 500),
          }),
        );
      }
    } else if (type === "function_call") {
      events.push(
        createEvent("tool-call", runId, roleId, {
          kind: "tool-call",
          tool: String(parsed.name ?? "unknown"),
          args: String(parsed.arguments ?? "").slice(0, 500),
        }),
      );
    } else if (type === "function_call_output") {
      events.push(
        createEvent("tool-output", runId, roleId, {
          kind: "tool-output",
          tool: String(parsed.name ?? "unknown"),
          output: String(parsed.output ?? "").slice(0, 1000),
          truncated: String(parsed.output ?? "").length > 1000,
        }),
      );
    }
    return events;
  }
}
