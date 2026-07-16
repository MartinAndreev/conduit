import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import path from "node:path";
import type { AgentAssignmentV1 } from "../../../domains/runs/types/agent-protocol.js";
import { redactSecrets } from "../../storage/security/secret-redaction.js";
import { parseNativeEvent } from "../services/native-event-parser.js";
import type {
  AgentCommunicationProvider,
  AgentCommunicationSession,
  CommunicationProviderInspection,
  CreateCommunicationSessionInput,
  PermissionResponse,
} from "../types/provider.js";
import type {
  BidirectionalProviderOptions,
  JsonRpcResponse,
  PendingPermissionRequest,
  PendingRpcRequest,
} from "../types/rpc-provider.js";
import type {
  ConduitRuntimeEvent,
  ConduitRuntimeEventType,
  NativeTerminalResult,
} from "../types/runtime-event.js";

const maxRecordBytes = 256_000;
const maxQueuedEvents = 512;
const finalResponseLimit = 256_000;
const databaseEnvironmentKey =
  /^(?:TURSO_|LIBSQL_|DATABASE_(?:URL|TOKEN)$|CONDUIT_DB)/i;

function environment(): NodeJS.ProcessEnv {
  return Object.fromEntries(
    Object.entries(process.env).filter(
      ([key]) => !databaseEnvironmentKey.test(key),
    ),
  );
}

function record(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function boundedNativeValue(value: unknown, limit: number): string {
  if (value === undefined || value === null) return "";
  const serialized =
    typeof value === "string"
      ? value
      : (JSON.stringify(value) ?? String(value));
  return redactSecrets(serialized).slice(0, limit);
}

function executable(options: BidirectionalProviderOptions): string | undefined {
  for (const candidate of options.executableCandidates) {
    const result = spawnSync(candidate, ["--version"], {
      encoding: "utf8",
      timeout: 5_000,
    });
    if (!result.error && result.status === 0) return candidate;
  }
  return undefined;
}

function detectedVersion(command: string): string | undefined {
  const result = spawnSync(command, ["--version"], {
    encoding: "utf8",
    timeout: 5_000,
  });
  return `${result.stdout || result.stderr}`.match(/\d+\.\d+\.\d+/)?.[0];
}

export class BidirectionalCommunicationProvider implements AgentCommunicationProvider {
  readonly id;

  constructor(private readonly options: BidirectionalProviderOptions) {
    this.id = options.id;
  }

  async inspect(): Promise<CommunicationProviderInspection> {
    const command = executable(this.options);
    if (!command) return this.inspection(undefined, "executable not found");
    const version = detectedVersion(command);
    if (!version || !this.options.verifiedVersions.includes(version)) {
      return this.inspection(
        version,
        `${version ?? "unknown version"} is not verified for ${this.options.protocol}`,
      );
    }
    return this.inspection(version);
  }

  async createSession(
    input: CreateCommunicationSessionInput,
  ): Promise<AgentCommunicationSession> {
    const inspection = await this.inspect();
    if (!inspection.availability.available)
      throw new Error(inspection.availability.reason);
    const command = executable(this.options);
    if (!command)
      throw new Error(`${this.options.runner} executable not found`);
    return new BidirectionalCommunicationSession(this.options, command, input);
  }

  private inspection(
    version?: string,
    unavailableReason?: string,
  ): CommunicationProviderInspection {
    return {
      providerId: this.id,
      runner: this.options.runner,
      ...(version ? { version } : {}),
      availability: unavailableReason
        ? {
            available: false,
            reason: `${this.options.runner} ${unavailableReason}`,
          }
        : { available: true },
      capability: {
        protocol: this.options.protocol,
        ...(this.options.protocol === "acp-stdio"
          ? { protocolVersion: "1" }
          : {}),
        bidirectional: true,
        sessions: true,
        permissions: this.options.protocol === "acp-stdio",
        cancellation: "native",
        finalResponseCapture: "native-final-message",
        telemetry: {
          lifecycle: "supported",
          activity: "supported",
          tools: "supported",
          commands: "supported",
          files: "unknown",
          permissions:
            this.options.protocol === "acp-stdio" ? "supported" : "unsupported",
          usage: "supported",
        },
      },
    };
  }
}

class BidirectionalCommunicationSession implements AgentCommunicationSession {
  readonly events: AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  >;
  nativeSessionId?: string;
  private child?: ChildProcess;
  private started = false;
  private submitted = false;
  private completed = false;
  private cancelled = false;
  private sequence = 0;
  private requestSequence = 0;
  private dropped = 0;
  private queue: ConduitRuntimeEvent[] = [];
  private wake?: () => void;
  private terminal?: NativeTerminalResult;
  private finalResponse = "";
  private acpMessages = new Map<string, string>();
  private acpThoughts = new Map<string, string>();
  private pending = new Map<string | number, PendingRpcRequest>();
  private permissions = new Map<string, PendingPermissionRequest>();

  constructor(
    private readonly options: BidirectionalProviderOptions,
    private readonly command: string,
    private readonly input: CreateCommunicationSessionInput,
  ) {
    this.events = this.stream();
  }

  async start(): Promise<void> {
    if (this.started) return;
    this.started = true;
    this.child = spawn(
      this.command,
      this.options.buildArgs({
        model: this.input.model,
        effort: this.input.effort,
        workspaceRoot: this.input.workspaceRoot,
      }),
      {
        cwd: this.input.workspaceRoot,
        stdio: ["pipe", "pipe", "pipe"],
        detached: process.platform !== "win32",
        env: environment(),
      },
    );
    this.attachProcess(this.child);
    this.enqueue(
      "protocol-lifecycle",
      { state: "negotiating" },
      "conduit.spawn",
    );
    if (this.options.protocol === "acp-stdio") await this.startAcp();
    else await this.startPiRpc();
    if (this.input.signal?.aborted) await this.cancel();
    else
      this.input.signal?.addEventListener(
        "abort",
        () => {
          void this.cancel();
        },
        { once: true },
      );
  }

  async submit(assignment: AgentAssignmentV1): Promise<void> {
    if (!this.started || !this.child)
      throw new Error("Communication session must be started before submit");
    if (this.submitted) throw new Error("Assignment was already submitted");
    if (assignment.assignmentId !== this.input.assignment.assignmentId)
      throw new Error("Submitted assignment does not match session assignment");
    this.submitted = true;
    const prompt = `Perform only this authoritative AgentAssignmentV1. Read its contextReferences inside the workspace. Return exactly one AgentResponseV1 JSON object as the final response.\n${JSON.stringify(assignment)}`;
    if (this.options.protocol === "acp-stdio") {
      void this.request("session/prompt", {
        sessionId: this.nativeSessionId,
        prompt: [{ type: "text", text: prompt }],
      })
        .then((result) => this.completeAcpPrompt(result))
        .catch((error) => this.fail(error));
    } else {
      await this.request("prompt", { message: prompt });
    }
    this.enqueue("protocol-lifecycle", { state: "accepted" }, "conduit.submit");
  }

  async respondToPermission(response: PermissionResponse): Promise<void> {
    this.sendPermissionDecision(response.requestId, response.decision);
  }

  private permissionTargetsWorkspace(toolCall: unknown): boolean {
    const call = record(toolCall);
    const locations = Array.isArray(call?.locations)
      ? call.locations.map(record).filter(Boolean)
      : [];
    const paths = locations
      .map((location) => location?.path)
      .filter(
        (candidate): candidate is string => typeof candidate === "string",
      );
    if (!paths.length) return true;
    const workspace = path.resolve(this.input.workspaceRoot);
    return paths.every((candidate) => {
      const resolved = path.resolve(workspace, candidate);
      const relative = path.relative(workspace, resolved);
      if (
        relative === ".." ||
        relative.startsWith(`..${path.sep}`) ||
        path.isAbsolute(relative)
      )
        return false;
      return !this.input.assignment.forbiddenPaths.some(
        (forbidden) =>
          relative === forbidden ||
          relative.startsWith(`${forbidden}${path.sep}`),
      );
    });
  }

  private sendPermissionDecision(
    requestId: string,
    decision: PermissionResponse["decision"],
  ): void {
    const pending = this.permissions.get(requestId);
    if (!pending) throw new Error(`Unknown permission request: ${requestId}`);
    const option = pending.options.find((candidate) => {
      const kind = String(candidate.kind ?? "");
      return decision === "approved"
        ? kind === "allow_once" || kind === "allow_always"
        : kind === "reject_once" || kind === "reject_always";
    });
    if (!option) throw new Error(`No ${decision} option is available`);
    this.write({
      jsonrpc: "2.0",
      id: pending.nativeRequestId,
      result: {
        outcome: { outcome: "selected", optionId: option.optionId },
      },
    });
    this.permissions.delete(requestId);
    this.enqueue(
      "permission",
      { requestId, state: decision },
      "session/request_permission.response",
      requestId,
    );
  }

  async cancel(): Promise<void> {
    if (this.cancelled) return;
    this.cancelled = true;
    if (this.options.protocol === "acp-stdio" && this.nativeSessionId) {
      this.write({
        jsonrpc: "2.0",
        method: "session/cancel",
        params: { sessionId: this.nativeSessionId },
      });
      for (const pending of this.permissions.values()) {
        this.write({
          jsonrpc: "2.0",
          id: pending.nativeRequestId,
          result: { outcome: { outcome: "cancelled" } },
        });
      }
      this.permissions.clear();
    } else if (this.options.protocol === "rpc-stdio") {
      await this.request("abort", {}).catch(() => undefined);
    }
    this.enqueue(
      "protocol-lifecycle",
      { state: "cancelling" },
      "conduit.cancel",
    );
    const child = this.child;
    setTimeout(() => this.terminate(child, "SIGTERM"), 3_000).unref();
    setTimeout(() => this.terminate(child, "SIGKILL"), 6_000).unref();
  }

  async close(): Promise<void> {
    const child = this.child;
    if (child?.stdin && !child.stdin.destroyed) child.stdin.end();
    if (child && child.exitCode === null) this.terminate(child, "SIGTERM");
  }

  private async startAcp(): Promise<void> {
    const initialized = record(
      await this.request("initialize", {
        protocolVersion: 1,
        clientCapabilities: {},
        clientInfo: { name: "conduit", version: "0.6.0" },
      }),
    );
    if (initialized?.protocolVersion !== 1)
      throw new Error("ACP protocol version 1 was not negotiated");
    const session = record(
      await this.request("session/new", {
        cwd: path.resolve(this.input.workspaceRoot),
        mcpServers: [],
      }),
    );
    if (typeof session?.sessionId !== "string")
      throw new Error("ACP did not return a session ID");
    this.nativeSessionId = session.sessionId;
    const options = Array.isArray(session.configOptions)
      ? session.configOptions.map(record).filter(Boolean)
      : [];
    if (this.input.model) {
      const model = options.find((option) => option?.id === "model");
      const values = Array.isArray(model?.options)
        ? model.options.map(record).filter(Boolean)
        : [];
      if (!values.some((option) => option?.value === this.input.model))
        throw new Error(`ACP model is unavailable: ${this.input.model}`);
      await this.request("session/set_config_option", {
        sessionId: this.nativeSessionId,
        configId: "model",
        value: this.input.model,
      });
    }
    if (
      this.input.effort &&
      options.some((option) => option?.id === "effort")
    ) {
      await this.request("session/set_config_option", {
        sessionId: this.nativeSessionId,
        configId: "effort",
        value: this.input.effort,
      });
    }
    this.enqueue(
      "protocol-lifecycle",
      { state: "session-created" },
      "session/new",
    );
  }

  private async startPiRpc(): Promise<void> {
    const state = record(await this.request("get_state", {}));
    if (!state) throw new Error("Pi RPC did not return session state");
    if (typeof state.sessionId === "string")
      this.nativeSessionId = state.sessionId;
    this.enqueue(
      "protocol-lifecycle",
      { state: "session-created" },
      "get_state",
    );
  }

  private completeAcpPrompt(value: unknown): void {
    this.flushAcpText();
    const result = record(value);
    const stopReason = String(result?.stopReason ?? "unknown");
    const candidate = this.finalResponse.trim();
    this.enqueue(
      "protocol-lifecycle",
      { state: "completed", stopReason },
      "session/prompt.response",
    );
    this.finish({
      status: this.cancelled ? "cancelled" : "completed",
      finalResponseCandidate: candidate || undefined,
      nativeSessionId: this.nativeSessionId,
      diagnostics: [],
    });
  }

  private attachProcess(child: ChildProcess): void {
    let stdout = "";
    child.stdout?.on("data", (chunk: Buffer | string) => {
      stdout += redactSecrets(String(chunk));
      if (
        Buffer.byteLength(stdout) > maxRecordBytes &&
        !stdout.includes("\n")
      ) {
        stdout = "";
        this.enqueue(
          "warning",
          { code: "OVERSIZED_NATIVE_RECORD" },
          "parser.oversized",
        );
      }
      let newline = stdout.indexOf("\n");
      while (newline >= 0) {
        const line = stdout.slice(0, newline).replace(/\r$/, "");
        stdout = stdout.slice(newline + 1);
        if (line.trim()) this.consumeLine(line);
        newline = stdout.indexOf("\n");
      }
    });
    child.stderr?.on("data", (chunk: Buffer | string) => {
      const message = redactSecrets(String(chunk)).trim();
      if (message)
        this.enqueue(
          "warning",
          {
            message: message.slice(0, 2_000),
            truncated: message.length > 2_000,
          },
          "stderr",
        );
    });
    child.on("error", (error) => this.fail(error));
    child.on("close", (code, signal) => {
      if (!this.completed) {
        this.finish({
          status: this.cancelled ? "cancelled" : "failed",
          exitCode: code ?? undefined,
          signal: signal ?? undefined,
          nativeSessionId: this.nativeSessionId,
          diagnostics: [
            `${this.options.runner} exited before terminal protocol state`,
          ],
        });
      }
    });
  }

  private consumeLine(line: string): void {
    if (Buffer.byteLength(line) > maxRecordBytes) {
      this.enqueue(
        "warning",
        { code: "OVERSIZED_NATIVE_RECORD" },
        "parser.oversized",
      );
      return;
    }
    let value: Record<string, unknown>;
    try {
      const parsed = JSON.parse(line) as unknown;
      if (!record(parsed)) throw new Error("record is not an object");
      value = parsed as Record<string, unknown>;
    } catch {
      this.enqueue(
        "warning",
        { code: "MALFORMED_NATIVE_RECORD", preview: line.slice(0, 500) },
        "parser.malformed",
      );
      return;
    }
    if (
      value.id !== undefined &&
      (value.result !== undefined || value.error !== undefined)
    ) {
      this.resolveResponse(value as unknown as JsonRpcResponse);
      return;
    }
    if (this.options.protocol === "acp-stdio") this.consumeAcp(value);
    else this.consumePi(value);
  }

  private consumeAcp(value: Record<string, unknown>): void {
    const method = String(value.method ?? "unknown");
    const params = record(value.params);
    if (method === "session/request_permission" && value.id !== undefined) {
      const requestId = String(value.id);
      const options = Array.isArray(params?.options)
        ? params.options
            .map(record)
            .filter((item): item is Record<string, unknown> => Boolean(item))
        : [];
      this.permissions.set(requestId, {
        nativeRequestId: value.id as string | number,
        options,
      });
      this.enqueue(
        "permission",
        { requestId, state: "requested", toolCall: params?.toolCall, options },
        method,
        requestId,
      );
      this.sendPermissionDecision(
        requestId,
        this.permissionTargetsWorkspace(params?.toolCall)
          ? "approved"
          : "denied",
      );
      return;
    }
    if (method !== "session/update") return;
    const update = record(params?.update);
    const kind = String(update?.sessionUpdate ?? "unknown");
    const correlationId =
      typeof update?.toolCallId === "string" ? update.toolCallId : undefined;
    if (kind === "agent_message_chunk" || kind === "agent_thought_chunk") {
      const content = record(update?.content);
      const text = typeof content?.text === "string" ? content.text : "";
      const messageId =
        typeof update?.messageId === "string" ? update.messageId : kind;
      const buffers =
        kind === "agent_message_chunk" ? this.acpMessages : this.acpThoughts;
      buffers.set(
        messageId,
        `${buffers.get(messageId) ?? ""}${text}`.slice(-finalResponseLimit),
      );
      return;
    }
    if (kind === "tool_call" || kind === "tool_call_update") {
      this.flushAcpText();
      this.enqueue(
        "tool-call",
        {
          state:
            update?.status ?? (kind === "tool_call" ? "started" : "update"),
          tool: update?.title ?? update?.kind ?? "unknown",
          input: boundedNativeValue(update?.rawInput, 1_000),
          output: boundedNativeValue(update?.content, 4_000),
        },
        kind,
        correlationId,
      );
      return;
    }
    if (kind === "plan") {
      this.enqueue("plan-update", { entries: update?.entries }, kind);
      return;
    }
    if (kind === "usage_update") {
      this.enqueue("usage", update ?? {}, kind);
      return;
    }
  }

  private flushAcpText(): void {
    for (const text of this.acpThoughts.values()) {
      const summary = text.trim();
      if (summary)
        this.enqueue(
          "agent-activity",
          { summary: summary.slice(0, 4_000) },
          "agent_thought",
        );
    }
    this.acpThoughts.clear();
    for (const text of this.acpMessages.values()) {
      const message = text.trim();
      if (!message) continue;
      this.finalResponse = message.slice(0, finalResponseLimit);
      const finalCandidate = message.startsWith("{");
      this.enqueue(
        finalCandidate ? "final-response-candidate" : "agent-activity",
        finalCandidate
          ? { message: "Structured final response received" }
          : { message: message.slice(0, 4_000) },
        "agent_message",
      );
    }
    this.acpMessages.clear();
  }

  private consumePi(value: Record<string, unknown>): void {
    const nativeType = String(value.type ?? "unknown");
    if (nativeType === "response") {
      const id = value.id;
      if (id !== undefined)
        this.resolveResponse(value as unknown as JsonRpcResponse);
      return;
    }
    for (const parsed of parseNativeEvent("pi", value)) {
      if (parsed.finalResponseCandidate)
        this.finalResponse = parsed.finalResponseCandidate.slice(
          0,
          finalResponseLimit,
        );
      this.enqueue(
        parsed.type,
        parsed.payload,
        parsed.nativeType,
        parsed.correlationId,
      );
    }
    if (nativeType === "agent_settled") {
      void this.request("get_last_assistant_text", {})
        .then((result) => {
          const text = record(result)?.text;
          const candidate =
            typeof text === "string"
              ? text.slice(0, finalResponseLimit)
              : this.finalResponse;
          this.finish({
            status: this.cancelled ? "cancelled" : "completed",
            finalResponseCandidate: candidate || undefined,
            nativeSessionId: this.nativeSessionId,
            diagnostics: [],
          });
        })
        .catch((error) => this.fail(error));
    }
  }

  private request(
    method: string,
    params: Record<string, unknown>,
  ): Promise<unknown> {
    const id = ++this.requestSequence;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.write(
        this.options.protocol === "acp-stdio"
          ? { jsonrpc: "2.0", id, method, params }
          : { id, type: method, ...params },
      );
    });
  }

  private resolveResponse(response: JsonRpcResponse): void {
    const pending = this.pending.get(response.id);
    if (!pending) return;
    this.pending.delete(response.id);
    if (response.error !== undefined || record(response)?.success === false) {
      const error = record(response.error);
      pending.reject(
        new Error(
          redactSecrets(
            String(
              error?.message ??
                record(response)?.error ??
                "Native request failed",
            ),
          ),
        ),
      );
      return;
    }
    if (this.options.protocol === "rpc-stdio") {
      pending.resolve(record(response)?.data ?? response.result ?? {});
    } else pending.resolve(response.result);
  }

  private write(value: Readonly<Record<string, unknown>>): void {
    if (!this.child?.stdin || this.child.stdin.destroyed)
      throw new Error("Native protocol stdin is unavailable");
    this.child.stdin.write(`${JSON.stringify(value)}\n`);
  }

  private enqueue(
    type: ConduitRuntimeEventType,
    payload: Readonly<Record<string, unknown>>,
    nativeType?: string,
    correlationId?: string,
  ): void {
    if (this.queue.length >= maxQueuedEvents) {
      this.dropped += 1;
      return;
    }
    this.queue.push({
      version: "1.0",
      sequence: ++this.sequence,
      receivedAt: new Date().toISOString(),
      context: {
        runId: this.input.assignment.assignmentId.split(":")[0],
        roleId: this.input.assignment.role,
        ...(this.nativeSessionId ? { sessionId: this.nativeSessionId } : {}),
      },
      provenance:
        nativeType?.startsWith("conduit.") || nativeType?.startsWith("parser.")
          ? "conduit-observed"
          : "runner-reported",
      type,
      payload,
      native: {
        protocol: this.options.protocol,
        ...(this.options.protocol === "acp-stdio"
          ? { protocolVersion: "1" }
          : {}),
        ...(nativeType ? { nativeType } : {}),
        ...(correlationId ? { nativeCorrelationId: correlationId } : {}),
      },
    });
    this.wake?.();
    this.wake = undefined;
  }

  private fail(cause: unknown): void {
    const message = redactSecrets(
      cause instanceof Error ? cause.message : String(cause),
    );
    this.enqueue("native-error", { message }, "conduit.protocol-error");
    this.finish({
      status: this.cancelled ? "cancelled" : "failed",
      nativeSessionId: this.nativeSessionId,
      diagnostics: [message],
    });
  }

  private finish(terminal: NativeTerminalResult): void {
    if (this.completed) return;
    if (this.dropped)
      this.enqueue(
        "dropped-events",
        { count: this.dropped },
        "conduit.backpressure",
      );
    this.completed = true;
    this.terminal = terminal;
    for (const pending of this.pending.values())
      pending.reject(
        new Error("Native session completed before request response"),
      );
    this.pending.clear();
    this.wake?.();
    this.wake = undefined;
  }

  private terminate(
    child: ChildProcess | undefined,
    signal: NodeJS.Signals,
  ): void {
    if (!child || child.exitCode !== null || child.killed) return;
    try {
      if (process.platform !== "win32" && child.pid)
        process.kill(-child.pid, signal);
      else child.kill(signal);
    } catch {
      // Process termination is best effort after native cancellation.
    }
  }

  private async *stream(): AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  > {
    while (!this.completed || this.queue.length) {
      const event = this.queue.shift();
      if (event) {
        yield event;
        continue;
      }
      await new Promise<void>((resolve) => {
        this.wake = resolve;
      });
    }
    return (
      this.terminal ?? {
        status: "failed",
        diagnostics: ["Native protocol ended without a terminal result"],
      }
    );
  }
}
