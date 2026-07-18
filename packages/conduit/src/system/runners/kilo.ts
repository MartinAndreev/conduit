import { spawnSync } from "node:child_process";
import type { RunnerAdapter, RunnerAvailability } from "./adapter.js";
import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";
import { createEvent } from "./events.js";
import { JsonLineOutputParser } from "./jsonl-parser.js";

export class KiloAdapter implements RunnerAdapter {
  readonly name = "kilo";
  readonly command = "kilo";

  async checkAvailability(): Promise<RunnerAvailability> {
    const result = spawnSync("which", ["kilo"], { encoding: "utf8" });
    if (result.status !== 0) {
      return { available: false, reason: "kilo executable not found in PATH" };
    }
    return { available: true };
  }

  buildArgs(promptFile: string, model?: string): readonly string[] {
    const args = ["run", "--pure", "--format", "json"];
    if (model) args.push("--model", model);
    args.push(`Read ${promptFile} and perform only your assigned task.`);
    return args;
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
        events.push(...this.parseJson(parsed, runId, roleId));
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

  private parseJson(
    parsed: Record<string, unknown>,
    runId: string,
    roleId: string,
  ): RunnerEvent[] {
    const events: RunnerEvent[] = [];
    const type = parsed.type as string | undefined;

    if (type === "text" && typeof parsed.text === "string") {
      events.push(
        createEvent("activity", runId, roleId, {
          kind: "activity",
          message: parsed.text.slice(0, 500),
        }),
      );
    } else if (type === "tool_call") {
      events.push(
        createEvent("tool-call", runId, roleId, {
          kind: "tool-call",
          tool: String(parsed.tool ?? "unknown"),
          args: JSON.stringify(parsed.parameters ?? {}).slice(0, 500),
        }),
      );
    } else if (type === "tool_result") {
      events.push(
        createEvent("tool-output", runId, roleId, {
          kind: "tool-output",
          tool: String(parsed.tool ?? "unknown"),
          output: String(parsed.output ?? "").slice(0, 1000),
          truncated: String(parsed.output ?? "").length > 1000,
        }),
      );
    } else if (type === "file_write") {
      events.push(
        createEvent("file-change", runId, roleId, {
          kind: "file-change",
          path: String(parsed.path ?? ""),
          additions: Number(parsed.additions ?? 0),
          deletions: Number(parsed.deletions ?? 0),
        }),
      );
    }
    return events;
  }
}
