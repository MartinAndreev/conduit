import type { RunnerEvent } from "../../domains/runs/types/runner-events.js";

export interface RunnerOutputParser {
  push(chunk: string): readonly RunnerEvent[];
  flush(): readonly RunnerEvent[];
  readonly finalResponse: string | undefined;
}

export class JsonLineOutputParser implements RunnerOutputParser {
  private buffer = "";
  private finalValue: string | undefined;

  constructor(
    private readonly parseLine: (line: string) => readonly RunnerEvent[],
    private readonly fallback: (line: string) => RunnerEvent,
  ) {}

  get finalResponse(): string | undefined {
    return this.finalValue;
  }

  push(chunk: string): readonly RunnerEvent[] {
    this.buffer += chunk;
    const events: RunnerEvent[] = [];
    let newline = this.buffer.indexOf("\n");

    while (newline >= 0) {
      const line = this.buffer.slice(0, newline).trim();
      this.buffer = this.buffer.slice(newline + 1);
      if (line) events.push(...this.consume(line));
      newline = this.buffer.indexOf("\n");
    }

    return events;
  }

  flush(): readonly RunnerEvent[] {
    const line = this.buffer.trim();
    this.buffer = "";
    return line ? this.consume(line) : [];
  }

  private consume(line: string): readonly RunnerEvent[] {
    try {
      const parsed = JSON.parse(line) as Record<string, unknown>;
      const final = extractFinalResponse(parsed);
      if (final) this.finalValue = final;
      return this.parseLine(line);
    } catch {
      return [this.fallback(line)];
    }
  }
}

export function extractFinalResponse(
  parsed: Record<string, unknown>,
): string | undefined {
  for (const key of [
    "finalResponse",
    "final_response",
    "lastMessage",
    "last_message",
    "result",
    "output",
  ] as const) {
    if (typeof parsed[key] === "string" && parsed[key].trim().startsWith("{")) {
      return parsed[key];
    }
  }

  if (
    (parsed.type === "final" || parsed.type === "message") &&
    typeof parsed.content === "string" &&
    parsed.content.trim().startsWith("{")
  ) {
    return parsed.content;
  }

  if (
    parsed.role === "assistant" &&
    typeof parsed.content === "string" &&
    parsed.content.trim().startsWith("{")
  ) {
    return parsed.content;
  }

  return undefined;
}
