import type {
  ArchitectEvent,
  ParsedArchitectLine,
} from "../types/architect-event.js";

const eventBoundaries = new Set([
  "exec",
  "analysis",
  "codex",
  "apply patch",
  "patch: completed",
]);

const activityDescriptions = new Map<string, string>([
  ["analysis", "Analyzing project context"],
  ["codex", "Refining feature specification"],
]);

function record(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, unknown>;
}

function itemText(item: Record<string, unknown>): string | undefined {
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
          return record(entry)?.text;
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

function parseJsonEvent(
  line: string,
  index: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  let source: Record<string, unknown>;
  try {
    const parsed = record(JSON.parse(line));
    if (!parsed) {
      return undefined;
    }
    source = parsed;
  } catch {
    return undefined;
  }

  const eventType = source.type;
  const item = record(source.item);
  const itemType = item?.type;
  const text = item ? itemText(item) : undefined;
  if (
    (eventType === "item.started" || eventType === "item.completed") &&
    itemType === "reasoning" &&
    text
  ) {
    return {
      event: { type: "thought", timestamp, content: text },
      nextIndex: index + 1,
    };
  }
  if (
    (eventType === "item.started" || eventType === "item.completed") &&
    itemType === "agent_message" &&
    text
  ) {
    const content = text.trim().startsWith("{")
      ? "Structured response received"
      : text;
    return {
      event: {
        type: "activity",
        timestamp,
        content,
      },
      nextIndex: index + 1,
    };
  }
  if (
    (eventType === "item.started" || eventType === "item.completed") &&
    itemType === "command_execution"
  ) {
    const command = String(item?.command ?? "command");
    if (eventType === "item.started") {
      return {
        event: { type: "tool-call", timestamp, content: command },
        nextIndex: index + 1,
      };
    }
    return {
      event: {
        type: "tool-output",
        timestamp,
        content: `${command} · ${String(item?.status ?? "completed")}`,
      },
      nextIndex: index + 1,
    };
  }
  if (eventType === "thread.started") {
    return {
      event: {
        type: "lifecycle",
        timestamp,
        content: "Architect session started",
      },
      nextIndex: index + 1,
    };
  }
  if (eventType === "turn.started" || eventType === "turn.completed") {
    const content =
      eventType === "turn.started"
        ? "Architect turn started"
        : "Architect turn completed";
    return {
      event: {
        type: "lifecycle",
        timestamp,
        content,
      },
      nextIndex: index + 1,
    };
  }
  if (
    eventType === "message" &&
    source.role === "assistant" &&
    typeof source.content === "string"
  ) {
    return {
      event: {
        type: "thought",
        timestamp,
        content: source.content,
      },
      nextIndex: index + 1,
    };
  }
  return undefined;
}

function parseToolCall(
  lines: readonly string[],
  index: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  if (lines[index] !== "exec") {
    return undefined;
  }
  return {
    event: {
      type: "tool-call",
      timestamp,
      content: lines[index + 1] ?? "",
    },
    nextIndex: index + 2,
  };
}

function parseActivity(
  line: string,
  index: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  if (line === "apply patch") {
    return {
      event: {
        type: "patch",
        timestamp,
        content: "Applying specification patch",
      },
      nextIndex: index + 1,
    };
  }
  const content = activityDescriptions.get(line);
  if (!content) {
    return undefined;
  }
  return {
    event: { type: "activity", timestamp, content },
    nextIndex: index + 1,
  };
}

function parseLifecycle(
  line: string,
  index: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  if (line !== "patch: completed") {
    return undefined;
  }
  return {
    event: {
      type: "lifecycle",
      timestamp,
      content: "Patch completed",
    },
    nextIndex: index + 1,
  };
}

function isEventBoundary(line: string): boolean {
  return eventBoundaries.has(line) || line.startsWith("diff --git ");
}

function parsePatch(
  lines: readonly string[],
  startIndex: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  if (!lines[startIndex].startsWith("diff --git ")) {
    return undefined;
  }

  const diffLines = [lines[startIndex]];
  let nextIndex = startIndex + 1;
  while (nextIndex < lines.length && !isEventBoundary(lines[nextIndex])) {
    diffLines.push(lines[nextIndex]);
    nextIndex += 1;
  }

  const diff = diffLines.join("\n");
  const files = [
    ...new Set(
      [...diff.matchAll(/^diff --git a\/(.+?) b\//gm)].map((match) => match[1]),
    ),
  ];
  return {
    event: {
      type: "patch",
      timestamp,
      content: `Applied patch: ${files.length} file${files.length === 1 ? "" : "s"}`,
      files,
      diff,
    },
    nextIndex,
  };
}

function parseLine(
  lines: readonly string[],
  index: number,
  timestamp: string,
): ParsedArchitectLine | undefined {
  const line = lines[index];
  return (
    parseJsonEvent(line, index, timestamp) ??
    parseToolCall(lines, index, timestamp) ??
    parseActivity(line, index, timestamp) ??
    parseLifecycle(line, index, timestamp) ??
    parsePatch(lines, index, timestamp)
  );
}

function removeDuplicateEvents(
  events: readonly ArchitectEvent[],
): ArchitectEvent[] {
  const patchKeys = new Set<string>();
  return events.filter((event, index) => {
    const previous = events[index - 1];
    if (
      previous &&
      previous.type === event.type &&
      previous.content === event.content
    ) {
      return false;
    }
    if (event.type !== "patch" || !event.files) {
      return true;
    }
    const key = [...event.files].sort().join(",");
    if (patchKeys.has(key)) {
      return false;
    }
    patchKeys.add(key);
    return true;
  });
}

export function normalizeArchitectEvents(
  events: readonly ArchitectEvent[],
): ArchitectEvent[] {
  const normalized = events.map((event) => {
    const content = activityDescriptions.get(event.content) ?? event.content;
    return content === event.content ? event : { ...event, content };
  });
  return removeDuplicateEvents(normalized);
}

export function extractArchitectEvents(
  transcript: string,
  timestamp = new Date().toISOString(),
): ArchitectEvent[] {
  const events: ArchitectEvent[] = [];
  const lines = transcript.split("\n");
  let index = 0;

  while (index < lines.length) {
    const parsed = parseLine(lines, index, timestamp);
    if (!parsed) {
      index += 1;
      continue;
    }
    events.push(parsed.event);
    index = parsed.nextIndex;
  }

  return normalizeArchitectEvents(events);
}
