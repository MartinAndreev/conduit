import { spawnSync } from "node:child_process";
import type { RunnerAdapter, RunnerAvailability } from "./adapter.js";
import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";
import { createEvent } from "./events.js";

export class PiAdapter implements RunnerAdapter {
  readonly name = "pi";
  readonly command = "pi";

  async checkAvailability(): Promise<RunnerAvailability> {
    const result = spawnSync("which", ["pi"], { encoding: "utf8" });
    if (result.status !== 0) {
      return { available: false, reason: "pi executable not found in PATH" };
    }
    return { available: true };
  }

  buildArgs(promptFile: string, model?: string): readonly string[] {
    const args: string[] = [];
    if (model) args.push("--model", model);
    args.push("-p", `Read ${promptFile} and perform only your assigned task.`);
    return args;
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
    } else if (type === "tool_use") {
      events.push(
        createEvent("tool-call", runId, roleId, {
          kind: "tool-call",
          tool: String(parsed.name ?? "unknown"),
          args: JSON.stringify(parsed.args ?? {}).slice(0, 500),
        }),
      );
    } else if (type === "tool_result") {
      events.push(
        createEvent("tool-output", runId, roleId, {
          kind: "tool-output",
          tool: String(parsed.name ?? "unknown"),
          output: String(parsed.result ?? "").slice(0, 1000),
          truncated: String(parsed.result ?? "").length > 1000,
        }),
      );
    } else if (type === "file_edit") {
      events.push(
        createEvent("file-change", runId, roleId, {
          kind: "file-change",
          path: String(parsed.file ?? ""),
          additions: Number(parsed.additions ?? 0),
          deletions: Number(parsed.deletions ?? 0),
        }),
      );
    }
    return events;
  }
}
