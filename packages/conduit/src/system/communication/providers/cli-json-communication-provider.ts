import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { AgentAssignmentV1 } from "../../../domains/runs/types/agent-protocol.js";
import { redactSecrets } from "../../storage/security/secret-redaction.js";
import { agentAssignmentPrompt } from "../services/agent-assignment-prompt.js";
import { parseNativeEvent } from "../services/native-event-parser.js";
import { agentResponseOutputSchema } from "../services/agent-response-output-schema.js";
import type {
  CliProviderOptions,
  ParsedNativeEvent,
} from "../types/cli-provider.js";
import type {
  AgentCommunicationProvider,
  AgentCommunicationSession,
  CommunicationProviderInspection,
  CreateCommunicationSessionInput,
  PermissionResponse,
} from "../types/provider.js";
import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "../types/runtime-event.js";

const maxRecordBytes = 256_000;
const maxQueuedEvents = 512;
const databaseEnvironmentKey =
  /^(?:TURSO_|LIBSQL_|DATABASE_(?:URL|TOKEN)$|CONDUIT_DB)/i;

function environment(): NodeJS.ProcessEnv {
  return Object.fromEntries(
    Object.entries(process.env).filter(
      ([key]) => !databaseEnvironmentKey.test(key),
    ),
  );
}

function executable(options: CliProviderOptions): string | undefined {
  for (const candidate of options.executableCandidates) {
    const result = spawnSync(candidate, ["--version"], {
      encoding: "utf8",
      timeout: 5_000,
    });
    if (!result.error && result.status === 0) return candidate;
  }
  return undefined;
}

export class CliJsonCommunicationProvider implements AgentCommunicationProvider {
  readonly id;
  constructor(private readonly options: CliProviderOptions) {
    this.id = options.id;
  }

  async inspect(): Promise<CommunicationProviderInspection> {
    const command = executable(this.options);
    if (!command)
      return this.inspection({
        available: false,
        reason: `${this.options.runner} executable not found`,
      });
    const result = spawnSync(command, ["--version"], {
      encoding: "utf8",
      timeout: 5_000,
    });
    const versionOutput = `${result.stdout || result.stderr}`
      .trim()
      .slice(0, 100);
    const version = versionOutput.match(/\d+\.\d+\.\d+/)?.[0];
    if (!version || !this.options.verifiedVersions.includes(version)) {
      return {
        ...this.inspection({
          available: false,
          reason: `${this.options.runner} ${version ?? "unknown version"} is not verified for ${this.options.protocol}`,
        }),
        ...(version ? { version } : {}),
      };
    }
    return { ...this.inspection({ available: true }), version };
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
    return new CliJsonCommunicationSession(this.options, command, input);
  }

  private inspection(
    availability: CommunicationProviderInspection["availability"],
  ): CommunicationProviderInspection {
    return {
      providerId: this.id,
      runner: this.options.runner,
      availability,
      degradedReason: "verified one-shot structured fallback",
      capability: {
        protocol: this.options.protocol,
        bidirectional: false,
        sessions: false,
        permissions: false,
        cancellation: "process",
        finalResponseCapture: this.options.finalResponseCapture,
        telemetry: {
          lifecycle: "supported",
          activity: "supported",
          tools: "supported",
          commands: "supported",
          files: "unknown",
          permissions: "unsupported",
          usage: "supported",
        },
      },
    };
  }
}

class CliJsonCommunicationSession implements AgentCommunicationSession {
  readonly nativeSessionId = undefined;
  readonly events: AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  >;
  private child?: ChildProcess;
  private spawnError?: string;
  private started = false;
  private completed = false;
  private cancelled = false;
  private sequence = 0;
  private queue: ConduitRuntimeEvent[] = [];
  private wake?: () => void;
  private terminal?: NativeTerminalResult;
  private finalResponseCandidate?: string;
  private temporaryDirectory?: string;
  private dropped = 0;

  constructor(
    private readonly options: CliProviderOptions,
    private readonly command: string,
    private readonly input: CreateCommunicationSessionInput,
  ) {
    this.events = this.stream();
  }

  async start(): Promise<void> {
    this.started = true;
  }

  async submit(assignment: AgentAssignmentV1): Promise<void> {
    if (!this.started)
      throw new Error("Communication session must be started before submit");
    if (this.child) throw new Error("Assignment was already submitted");
    if (assignment.assignmentId !== this.input.assignment.assignmentId)
      throw new Error("Submitted assignment does not match session assignment");
    this.temporaryDirectory = await mkdtemp(
      path.join(os.tmpdir(), "conduit-harness-"),
    );
    const outputFile = path.join(
      this.temporaryDirectory,
      "final-response.json",
    );
    const schemaFile = path.join(
      this.temporaryDirectory,
      "agent-response.schema.json",
    );
    await writeFile(
      schemaFile,
      `${JSON.stringify(agentResponseOutputSchema)}\n`,
      { mode: 0o600 },
    );
    const prompt = agentAssignmentPrompt(assignment);
    const args = this.options.buildArgs({
      prompt,
      model: this.input.model,
      effort: this.input.effort,
      outputFile,
      schemaFile,
    });
    this.enqueueParsed({
      type: "protocol-lifecycle",
      payload: { state: "starting", providerId: this.options.id },
      nativeType: "conduit.spawn",
    });
    this.child = spawn(this.command, args, {
      cwd: this.input.workspaceRoot,
      stdio: ["ignore", "pipe", "pipe"],
      detached: process.platform !== "win32",
      env: environment(),
    });
    let stdoutBuffer = "";
    this.child.stdout?.on("data", (chunk: Buffer | string) => {
      stdoutBuffer += redactSecrets(String(chunk));
      if (
        Buffer.byteLength(stdoutBuffer) > maxRecordBytes &&
        !stdoutBuffer.includes("\n")
      ) {
        stdoutBuffer = "";
        this.enqueueParsed({
          type: "warning",
          payload: {
            code: "OVERSIZED_NATIVE_RECORD",
            message: "Dropped an oversized native record",
          },
          nativeType: "parser.oversized",
        });
      }
      let newline = stdoutBuffer.indexOf("\n");
      while (newline >= 0) {
        const line = stdoutBuffer.slice(0, newline).trim();
        stdoutBuffer = stdoutBuffer.slice(newline + 1);
        if (line) this.consumeLine(line);
        newline = stdoutBuffer.indexOf("\n");
      }
    });
    this.child.stderr?.on("data", (chunk: Buffer | string) => {
      const message = redactSecrets(String(chunk)).trim();
      if (message)
        this.enqueueParsed({
          type: "warning",
          payload: {
            message: message.slice(0, 2_000),
            truncated: message.length > 2_000,
          },
          nativeType: "stderr",
        });
    });
    this.child.on("error", (error) => {
      this.spawnError = redactSecrets(error.message);
      this.enqueueParsed(
        {
          type: "native-error",
          payload: { message: this.spawnError },
          nativeType: "conduit.spawn-error",
        },
        true,
      );
    });
    this.child.on("close", async (code, signal) => {
      if (stdoutBuffer.trim()) this.consumeLine(stdoutBuffer.trim());
      const captured = await readFile(outputFile, "utf8").catch(() => "");
      if (captured.trim())
        this.finalResponseCandidate = redactSecrets(captured.trim());
      if (this.dropped)
        this.enqueueParsed(
          {
            type: "dropped-events",
            payload: { count: this.dropped },
            nativeType: "conduit.backpressure",
          },
          true,
        );
      this.enqueueParsed(
        {
          type: "process-outcome",
          payload: { exitCode: code ?? -1, signal, cancelled: this.cancelled },
          nativeType: "conduit.process",
        },
        true,
      );
      this.finish({
        status: this.cancelled
          ? "cancelled"
          : code === 0 && !this.spawnError
            ? "completed"
            : "failed",
        exitCode: code ?? undefined,
        signal: signal ?? undefined,
        finalResponseCandidate: this.finalResponseCandidate,
        diagnostics: this.spawnError
          ? [this.spawnError]
          : code === 0
            ? []
            : [
                `${this.options.runner} exited with ${code ?? signal ?? "unknown status"}`,
              ],
      });
    });
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

  async respondToPermission(_response: PermissionResponse): Promise<void> {
    throw new Error(
      `${this.options.protocol} does not support permission responses`,
    );
  }

  async cancel(): Promise<void> {
    this.cancelled = true;
    const child = this.child;
    if (!child || child.exitCode !== null) return;
    try {
      if (process.platform !== "win32" && child.pid) {
        process.kill(-child.pid, "SIGTERM");
      } else {
        child.kill("SIGTERM");
      }
    } catch {
      // The process may have completed before cancellation was delivered.
    }
    setTimeout(() => {
      if (child.exitCode !== null) return;
      try {
        if (process.platform !== "win32" && child.pid) {
          process.kill(-child.pid, "SIGKILL");
        } else {
          child.kill("SIGKILL");
        }
      } catch {
        // Process-group termination is best effort after the grace period.
      }
    }, 3_000).unref();
  }

  async close(): Promise<void> {
    if (!this.completed) await this.cancel();
    if (this.temporaryDirectory)
      await rm(this.temporaryDirectory, { recursive: true, force: true });
  }

  private consumeLine(line: string): void {
    if (Buffer.byteLength(line) > maxRecordBytes) {
      this.enqueueParsed({
        type: "warning",
        payload: { code: "OVERSIZED_NATIVE_RECORD" },
        nativeType: "parser.oversized",
      });
      return;
    }
    try {
      const value = JSON.parse(line) as unknown;
      if (value === null || typeof value !== "object" || Array.isArray(value))
        throw new Error("record is not an object");
      const parsed = parseNativeEvent(
        this.options.runner,
        value as Record<string, unknown>,
      );
      for (const event of parsed) this.enqueueParsed(event);
    } catch {
      this.enqueueParsed({
        type: "warning",
        payload: {
          code: "MALFORMED_NATIVE_RECORD",
          preview: line.slice(0, 500),
        },
        nativeType: "parser.malformed",
      });
    }
  }

  private enqueueParsed(parsed: ParsedNativeEvent, critical = false): void {
    if (parsed.finalResponseCandidate)
      this.finalResponseCandidate = parsed.finalResponseCandidate;
    const protectedEvent =
      parsed.type === "protocol-lifecycle" ||
      parsed.type === "native-error" ||
      parsed.type === "final-response-candidate" ||
      parsed.type === "process-outcome" ||
      parsed.type === "dropped-events";
    if (this.queue.length >= maxQueuedEvents) {
      if (!critical && !protectedEvent) {
        this.dropped += 1;
        return;
      }
      const replaceable = this.queue.findIndex(
        (event) =>
          event.type !== "native-error" &&
          event.type !== "final-response-candidate" &&
          event.type !== "process-outcome" &&
          event.type !== "dropped-events",
      );
      if (replaceable >= 0) {
        this.queue.splice(replaceable, 1);
        this.dropped += 1;
      } else if (
        parsed.type !== "final-response-candidate" &&
        parsed.type !== "process-outcome" &&
        parsed.type !== "dropped-events"
      ) {
        this.dropped += 1;
        return;
      } else {
        this.queue.shift();
        this.dropped += 1;
      }
    }
    const context = {
      runId: this.input.assignment.assignmentId.split(":")[0],
      roleId: this.input.assignment.role,
    };
    this.queue.push({
      version: "1.0",
      sequence: ++this.sequence,
      receivedAt: new Date().toISOString(),
      context,
      provenance:
        parsed.type === "process-outcome" ||
        parsed.type === "dropped-events" ||
        parsed.nativeType?.startsWith("conduit.") ||
        parsed.nativeType?.startsWith("parser.")
          ? "conduit-observed"
          : "runner-reported",
      type: parsed.type,
      payload: parsed.payload,
      native: {
        protocol: this.options.protocol,
        nativeType: parsed.nativeType,
        nativeCorrelationId: parsed.correlationId,
      },
    });
    this.wake?.();
    this.wake = undefined;
  }

  private finish(terminal: NativeTerminalResult): void {
    if (this.completed) return;
    this.completed = true;
    this.terminal = terminal;
    this.wake?.();
    this.wake = undefined;
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
        diagnostics: ["Communication session ended without a terminal result"],
      }
    );
  }
}
