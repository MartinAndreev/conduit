import { writeFile } from "node:fs/promises";
import { agentResponseToolInputSchema } from "./agent-response-output-schema.js";
import { parseAgentResponseV1 } from "../../../domains/runs/validation/agent-response-validator.js";

const captureEnvironmentKey = "CONDUIT_AGENT_RESPONSE_CAPTURE";
const readyEnvironmentKey = "CONDUIT_AGENT_RESPONSE_READY";
const fallbackProtocolVersion = "2024-11-05";
const toolName = "submit_agent_response";
const maximumRecordBytes = 256_000;
const maximumFeedbackCharacters = 2_000;

function response(id: unknown, result: unknown): void {
  process.stdout.write(`${JSON.stringify({ jsonrpc: "2.0", id, result })}\n`);
}

function errorResponse(id: unknown, code: number, message: string): void {
  process.stdout.write(
    `${JSON.stringify({ jsonrpc: "2.0", id, error: { code, message } })}\n`,
  );
}

function normalizeToolArguments(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) return value;
  const input = value as Readonly<Record<string, unknown>>;
  const verdict = input.verdict;
  const mapItems = (
    name: string,
    normalize: (item: unknown) => unknown,
  ): unknown =>
    Array.isArray(input[name]) ? input[name].map(normalize) : input[name];
  return {
    ...input,
    verdict:
      verdict === "none"
        ? null
        : {
            decision: verdict,
            rationale: input.verdictRationale,
          },
    findings: mapItems("findings", (finding) => {
      if (!finding || typeof finding !== "object" || Array.isArray(finding))
        return finding;
      const item = finding as Readonly<Record<string, unknown>>;
      return {
        ...item,
        evidence:
          typeof item.evidence === "string" ? [item.evidence] : item.evidence,
      };
    }),
    memoryProposals: mapItems("memoryProposals", (proposal) =>
      proposal && typeof proposal === "object" && !Array.isArray(proposal)
        ? { evidence: [], ...proposal }
        : proposal,
    ),
    globalPromotionProposals: mapItems(
      "globalPromotionProposals",
      (proposal) => {
        if (
          !proposal ||
          typeof proposal !== "object" ||
          Array.isArray(proposal)
        )
          return proposal;
        const item = proposal as Readonly<Record<string, unknown>>;
        return {
          ...item,
          evidence:
            typeof item.evidence === "string" ? [item.evidence] : item.evidence,
        };
      },
    ),
    verdictRationale: undefined,
  };
}

async function handleRequest(value: unknown): Promise<void> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  const request = value as Readonly<Record<string, unknown>>;
  if (request.id === undefined) return;
  if (request.method === "initialize") {
    const readyPath = process.env[readyEnvironmentKey];
    if (readyPath) await writeFile(readyPath, "ready\n", { mode: 0o600 });
    const params =
      request.params &&
      typeof request.params === "object" &&
      !Array.isArray(request.params)
        ? (request.params as Readonly<Record<string, unknown>>)
        : undefined;
    response(request.id, {
      protocolVersion:
        typeof params?.protocolVersion === "string"
          ? params.protocolVersion
          : fallbackProtocolVersion,
      capabilities: { tools: {} },
      serverInfo: { name: "conduit-agent-response", version: "1.0.0" },
    });
    return;
  }
  if (request.method === "ping") {
    response(request.id, {});
    return;
  }
  if (request.method === "tools/list") {
    response(request.id, {
      tools: [
        {
          name: toolName,
          description:
            "Submit the final AgentResponseV1 for this Conduit role. Call this only as the final action after completing the assignment.",
          inputSchema: agentResponseToolInputSchema,
        },
      ],
    });
    return;
  }
  if (request.method === "tools/call") {
    const params =
      request.params &&
      typeof request.params === "object" &&
      !Array.isArray(request.params)
        ? (request.params as Readonly<Record<string, unknown>>)
        : undefined;
    if (params?.name !== toolName) {
      errorResponse(request.id, -32602, "Unknown Conduit tool.");
      return;
    }
    const normalized = normalizeToolArguments(params.arguments);
    const parsed = parseAgentResponseV1(JSON.stringify(normalized));
    if (!parsed.valid || !parsed.value) {
      response(request.id, {
        isError: true,
        content: [
          {
            type: "text",
            text: `AgentResponseV1 validation failed: ${parsed.issues
              .slice(0, 8)
              .map((issue) => `${issue.path}: ${issue.message}`)
              .join("; ")}`.slice(0, maximumFeedbackCharacters),
          },
        ],
      });
      return;
    }
    const capturePath = process.env[captureEnvironmentKey];
    if (!capturePath) {
      errorResponse(
        request.id,
        -32603,
        "Conduit response capture is unavailable.",
      );
      return;
    }
    await writeFile(capturePath, `${JSON.stringify(parsed.value)}\n`, {
      mode: 0o600,
    });
    response(request.id, {
      content: [
        {
          type: "text",
          text: "AgentResponseV1 accepted. End the turn without additional work.",
        },
      ],
    });
    return;
  }
  errorResponse(request.id, -32601, "Method not found.");
}

async function handleLine(line: string): Promise<void> {
  if (!line.trim()) return;
  try {
    await handleRequest(JSON.parse(line));
  } catch {
    // Malformed notifications have no response target. Requests receive a
    // bounded generic error without echoing agent-authored content.
    try {
      const request = JSON.parse(line) as { id?: unknown };
      if (request.id !== undefined)
        errorResponse(request.id, -32603, "Conduit tool request failed.");
    } catch {
      // Ignore records that are not JSON-RPC messages.
    }
  }
}

export async function runAgentResponseMcpServer(): Promise<void> {
  let buffer = "";
  let droppingOversizedRecord = false;
  for await (const chunk of process.stdin) {
    let remaining = String(chunk);
    while (remaining) {
      if (droppingOversizedRecord) {
        const newline = remaining.indexOf("\n");
        if (newline < 0) break;
        droppingOversizedRecord = false;
        errorResponse(null, -32600, "MCP record exceeds the size limit.");
        remaining = remaining.slice(newline + 1);
        continue;
      }
      const newline = remaining.indexOf("\n");
      const segment = newline < 0 ? remaining : remaining.slice(0, newline);
      if (
        Buffer.byteLength(buffer) + Buffer.byteLength(segment) >
        maximumRecordBytes
      ) {
        buffer = "";
        if (newline < 0) {
          droppingOversizedRecord = true;
          break;
        }
        errorResponse(null, -32600, "MCP record exceeds the size limit.");
        remaining = remaining.slice(newline + 1);
        continue;
      }
      buffer += segment;
      if (newline < 0) break;
      await handleLine(buffer.replace(/\r$/, ""));
      buffer = "";
      remaining = remaining.slice(newline + 1);
    }
  }
  if (!droppingOversizedRecord && buffer) await handleLine(buffer);
}

export const agentResponseMcpCaptureEnvironmentKey = captureEnvironmentKey;
export const agentResponseMcpReadyEnvironmentKey = readyEnvironmentKey;
export const agentResponseMcpToolName = `conduit_${toolName}`;
