import type { ParsedNativeEvent } from "../types/cli-provider.js";

const previewLimit = 4_000;
const finalResponseLimit = 256_000;

function record(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function textContent(value: unknown): string | undefined {
  if (typeof value === "string") return value;
  if (!Array.isArray(value)) return undefined;
  const text = value
    .map((entry) => {
      const item = record(entry);
      return item?.type === "text" && typeof item.text === "string"
        ? item.text
        : undefined;
    })
    .filter((entry): entry is string => Boolean(entry))
    .join("\n");
  return text || undefined;
}

function bounded(value: unknown, limit = previewLimit): string {
  const serialized =
    typeof value === "string" ? value : JSON.stringify(value ?? "");
  return serialized.slice(0, limit);
}

export function parseNativeEvent(
  runner: "codex" | "opencode" | "pi" | "kilo",
  value: Record<string, unknown>,
): readonly ParsedNativeEvent[] {
  if (
    value.protocolVersion === "1.0" &&
    typeof value.status === "string" &&
    typeof value.summary === "string"
  ) {
    const candidate = JSON.stringify(value);
    return [
      {
        type: "final-response-candidate",
        payload: { message: "Structured final response received" },
        nativeType: "agent-response-v1",
        finalResponseCandidate: candidate,
      },
    ];
  }
  if (runner === "codex") return parseCodex(value);
  if (runner === "pi") return parsePi(value);
  return parseOpenCodeFamily(value);
}

function parseCodex(
  value: Record<string, unknown>,
): readonly ParsedNativeEvent[] {
  const nativeType = String(value.type ?? "unknown");
  const item = record(value.item);
  const itemType = String(item?.type ?? "");
  if (nativeType === "thread.started") {
    return [
      {
        type: "protocol-lifecycle",
        payload: { state: "session-created" },
        nativeType,
        nativeSessionId:
          typeof value.thread_id === "string" ? value.thread_id : undefined,
      },
    ];
  }
  if (nativeType === "turn.started" || nativeType === "turn.completed") {
    const events: ParsedNativeEvent[] = [
      {
        type: "protocol-lifecycle",
        payload: {
          state: nativeType === "turn.started" ? "running" : "completed",
        },
        nativeType,
      },
    ];
    const usage = record(value.usage);
    if (usage) events.push({ type: "usage", payload: usage, nativeType });
    return events;
  }
  if (
    nativeType === "turn.failed" ||
    nativeType === "error" ||
    itemType === "error"
  ) {
    return [
      {
        type: "native-error",
        payload: {
          message: bounded(
            item?.message ??
              record(value.error)?.message ??
              value.message ??
              "Runner failed",
            1_000,
          ),
        },
        nativeType,
      },
    ];
  }
  if (
    !item ||
    (nativeType !== "item.started" && nativeType !== "item.completed")
  )
    return [];
  const correlationId = typeof item.id === "string" ? item.id : undefined;
  if (itemType === "agent_message") {
    const fullText = bounded(item.text, finalResponseLimit);
    const text = fullText.slice(0, previewLimit);
    return [
      {
        type:
          nativeType === "item.completed" && fullText.trim().startsWith("{")
            ? "final-response-candidate"
            : "agent-activity",
        payload: { message: text, truncated: fullText.length > text.length },
        nativeType,
        correlationId,
        finalResponseCandidate: fullText.trim().startsWith("{")
          ? fullText.trim()
          : undefined,
      },
    ];
  }
  if (itemType === "reasoning")
    return [
      {
        type: "agent-activity",
        payload: { summary: bounded(item.text ?? item.summary) },
        nativeType,
        correlationId,
      },
    ];
  if (itemType === "command_execution")
    return [
      {
        type: "command",
        payload: {
          state: nativeType === "item.started" ? "started" : "completed",
          command: bounded(item.command, 1_000),
          output: bounded(item.aggregated_output),
          exitCode: item.exit_code,
        },
        nativeType,
        correlationId,
      },
    ];
  return [
    {
      type: "tool-call",
      payload: {
        state: nativeType.endsWith("started") ? "started" : "completed",
        tool: itemType,
      },
      nativeType,
      correlationId,
    },
  ];
}

function parseOpenCodeFamily(
  value: Record<string, unknown>,
): readonly ParsedNativeEvent[] {
  const nativeType = String(value.type ?? "unknown");
  const part = record(value.part) ?? value;
  const partType = String(part.type ?? nativeType);
  const correlationId = typeof part.id === "string" ? part.id : undefined;
  const sessionId =
    typeof value.sessionID === "string" ? value.sessionID : undefined;
  if (partType === "step-start")
    return [
      {
        type: "protocol-lifecycle",
        payload: { state: "running" },
        nativeType,
        nativeSessionId: sessionId,
      },
    ];
  if (partType === "step-finish") {
    const events: ParsedNativeEvent[] = [
      {
        type: "protocol-lifecycle",
        payload: { state: "completed", reason: part.reason },
        nativeType,
        nativeSessionId: sessionId,
      },
    ];
    if (record(part.tokens))
      events.push({
        type: "usage",
        payload: { tokens: part.tokens, cost: part.cost },
        nativeType,
      });
    return events;
  }
  if (partType === "text") {
    const fullText = bounded(part.text, finalResponseLimit);
    const text = fullText.slice(0, previewLimit);
    return [
      {
        type: fullText.trim().startsWith("{")
          ? "final-response-candidate"
          : "agent-activity",
        payload: { message: text, truncated: fullText.length > text.length },
        nativeType,
        correlationId,
        finalResponseCandidate: fullText.trim().startsWith("{")
          ? fullText.trim()
          : undefined,
        nativeSessionId: sessionId,
      },
    ];
  }
  if (partType === "reasoning")
    return [
      {
        type: "agent-activity",
        payload: { summary: bounded(part.text) },
        nativeType,
        correlationId,
      },
    ];
  if (
    partType === "tool" ||
    nativeType === "tool_use" ||
    nativeType === "tool_call"
  ) {
    const state = record(part.state);
    const input = state?.input ?? part.input ?? part.parameters;
    return [
      {
        type: "tool-call",
        payload: {
          state: state?.status ?? "update",
          tool: part.tool ?? part.name ?? "unknown",
          input: bounded(input),
        },
        nativeType,
        correlationId,
      },
    ];
  }
  if (nativeType === "error")
    return [
      {
        type: "native-error",
        payload: {
          message: bounded(record(value.error)?.data ?? value.error, 1_000),
        },
        nativeType,
      },
    ];
  return [];
}

function parsePi(value: Record<string, unknown>): readonly ParsedNativeEvent[] {
  const nativeType = String(value.type ?? "unknown");
  if (nativeType === "session")
    return [
      {
        type: "protocol-lifecycle",
        payload: { state: "session-created" },
        nativeType,
        nativeSessionId: typeof value.id === "string" ? value.id : undefined,
      },
    ];
  if (nativeType === "agent_start" || nativeType === "turn_start")
    return [
      { type: "protocol-lifecycle", payload: { state: "running" }, nativeType },
    ];
  if (nativeType === "agent_end" || nativeType === "turn_end") {
    const message = record(value.message);
    const text = textContent(message?.content)?.slice(0, finalResponseLimit);
    const events: ParsedNativeEvent[] = [
      {
        type: "protocol-lifecycle",
        payload: { state: "completed" },
        nativeType,
      },
    ];
    if (text)
      events.push({
        type: text.trim().startsWith("{")
          ? "final-response-candidate"
          : "agent-activity",
        payload: {
          message: bounded(text),
          truncated: text.length > previewLimit,
        },
        nativeType,
        finalResponseCandidate: text.trim().startsWith("{")
          ? text.trim()
          : undefined,
      });
    if (record(message?.usage))
      events.push({
        type: "usage",
        payload: record(message?.usage)!,
        nativeType,
      });
    return events;
  }
  if (nativeType === "message_end") {
    const message = record(value.message);
    if (message?.role !== "assistant") return [];
    const text = textContent(message.content)?.slice(0, finalResponseLimit);
    if (!text) return [];
    return [
      {
        type: text.trim().startsWith("{")
          ? "final-response-candidate"
          : "agent-activity",
        payload: {
          message: bounded(text),
          truncated: text.length > previewLimit,
        },
        nativeType,
        finalResponseCandidate: text.trim().startsWith("{")
          ? text.trim()
          : undefined,
      },
    ];
  }
  if (
    nativeType === "tool_execution_start" ||
    nativeType === "tool_execution_end"
  )
    return [
      {
        type: "tool-call",
        payload: {
          state: nativeType.endsWith("start") ? "started" : "completed",
          tool: value.toolName ?? value.tool ?? "unknown",
          input: bounded(value.args),
          output: bounded(value.result),
        },
        nativeType,
        correlationId:
          typeof value.toolCallId === "string" ? value.toolCallId : undefined,
      },
    ];
  if (nativeType === "error")
    return [
      {
        type: "native-error",
        payload: { message: bounded(value.error ?? value.message, 1_000) },
        nativeType,
      },
    ];
  return [];
}
