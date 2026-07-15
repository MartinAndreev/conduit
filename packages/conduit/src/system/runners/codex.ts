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
    const args = ["exec", "--json"];
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
    const item = this.record(parsed.item);
    const itemType = item?.type;
    const failureMessage = this.failureMessage(parsed, item);
    if (failureMessage) {
      events.push(
        createEvent("error", runId, roleId, {
          kind: "error",
          code: "RUNNER_ERROR",
          message: failureMessage.slice(0, 1_000),
          recoverable: false,
        }),
      );
      return events;
    }
    if (
      (type === "item.started" || type === "item.completed") &&
      item &&
      typeof itemType === "string"
    ) {
      events.push(...this.parseItem(type, itemType, item, runId, roleId));
    } else if (type === "message" && parsed.role === "assistant") {
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

  private failureMessage(
    parsed: Record<string, unknown>,
    item: Record<string, unknown> | undefined,
  ): string | undefined {
    const type = parsed.type;
    const itemType = item?.type;
    if (type !== "error" && type !== "turn.failed" && itemType !== "error") {
      return undefined;
    }
    const failure = this.record(parsed.error);
    const value = item?.message ?? failure?.message ?? parsed.message;
    if (typeof value !== "string" || !value.trim()) {
      return "Runner execution failed";
    }
    try {
      const nested = this.record(JSON.parse(value));
      const nestedError = this.record(nested?.error);
      if (typeof nestedError?.message === "string") {
        return nestedError.message;
      }
    } catch {
      // The runner may already provide a plain human-readable error.
    }
    return value.trim();
  }

  private parseItem(
    eventType: string,
    itemType: string,
    item: Record<string, unknown>,
    runId: string,
    roleId: string,
  ): RunnerEvent[] {
    const text = this.itemText(item);
    if (itemType === "reasoning" && text) {
      return [
        createEvent("activity", runId, roleId, {
          kind: "activity",
          message: `Reasoning summary: ${text.slice(0, 500)}`,
        }),
      ];
    }
    if (itemType === "agent_message" && text) {
      return [
        createEvent("activity", runId, roleId, {
          kind: "activity",
          message: text.slice(0, 500),
        }),
      ];
    }
    if (itemType === "command_execution") {
      const command = String(item.command ?? "command");
      if (eventType === "item.started") {
        return [
          createEvent("tool-call", runId, roleId, {
            kind: "tool-call",
            tool: "shell",
            args: command.slice(0, 500),
          }),
        ];
      }
      const output = String(item.aggregated_output ?? "");
      return [
        createEvent("tool-output", runId, roleId, {
          kind: "tool-output",
          tool: command.slice(0, 120),
          output: output.slice(0, 1_000),
          truncated: output.length > 1_000,
        }),
      ];
    }
    if (itemType === "web_search" && text) {
      return [
        createEvent("tool-call", runId, roleId, {
          kind: "tool-call",
          tool: "web search",
          args: text.slice(0, 500),
        }),
      ];
    }
    return [];
  }

  private record(value: unknown): Record<string, unknown> | undefined {
    return value !== null && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : undefined;
  }

  private itemText(item: Record<string, unknown>): string | undefined {
    for (const value of [item.text, item.summary, item.query]) {
      if (typeof value === "string" && value.trim()) {
        return value.trim();
      }
      if (Array.isArray(value)) {
        const text = value
          .map((entry) => {
            if (typeof entry === "string") {
              return entry;
            }
            return this.record(entry)?.text;
          })
          .filter((entry): entry is string => typeof entry === "string")
          .join("\n")
          .trim();
        if (text) {
          return text;
        }
      }
    }
    return undefined;
  }
}
