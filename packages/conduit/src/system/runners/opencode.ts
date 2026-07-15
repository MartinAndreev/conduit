import { spawnSync } from "node:child_process";
import type { RunnerAdapter, RunnerAvailability } from "./adapter.js";
import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";
import { createEvent } from "./events.js";
import { JsonLineOutputParser } from "./jsonl-parser.js";

export class OpenCodeAdapter implements RunnerAdapter {
  readonly name = "opencode";
  readonly command = "opencode";

  async checkAvailability(): Promise<RunnerAvailability> {
    const result = spawnSync("which", ["opencode"], { encoding: "utf8" });
    if (result.status !== 0) {
      return {
        available: false,
        reason: "opencode executable not found in PATH",
      };
    }
    return { available: true };
  }

  buildArgs(promptFile: string, model?: string): readonly string[] {
    const args = ["run"];
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

    if (type === "message" && typeof parsed.content === "string") {
      events.push(
        createEvent("activity", runId, roleId, {
          kind: "activity",
          message: parsed.content.slice(0, 500),
        }),
      );
    } else if (type === "tool_use") {
      events.push(
        createEvent("tool-call", runId, roleId, {
          kind: "tool-call",
          tool: String(parsed.name ?? "unknown"),
          args: JSON.stringify(parsed.input ?? {}).slice(0, 500),
        }),
      );
    } else if (type === "tool_result") {
      events.push(
        createEvent("tool-output", runId, roleId, {
          kind: "tool-output",
          tool: String(parsed.name ?? "unknown"),
          output: String(parsed.content ?? "").slice(0, 1000),
          truncated: String(parsed.content ?? "").length > 1000,
        }),
      );
    } else if (type === "file_change") {
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
