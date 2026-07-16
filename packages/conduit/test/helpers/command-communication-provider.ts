import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { CommunicationProviderId } from "../../src/system/communication/enums/communication-provider-id.js";
import type {
  AgentCommunicationProvider,
  AgentCommunicationSession,
  CreateCommunicationSessionInput,
  PermissionResponse,
} from "../../src/system/communication/types/provider.js";
import type {
  ConduitRuntimeEvent,
  NativeTerminalResult,
} from "../../src/system/communication/types/runtime-event.js";
import type { AgentAssignmentV1 } from "../../src/domains/runs/types/agent-protocol.js";

const executeFile = promisify(execFile);

export interface TestRoleCommand {
  readonly command: string;
  readonly args: readonly string[];
}

export function commandCommunicationProvider(
  commands: Readonly<Record<string, TestRoleCommand>>,
): AgentCommunicationProvider {
  return {
    id: CommunicationProviderId.CodexExec,
    async inspect() {
      return {
        providerId: CommunicationProviderId.CodexExec,
        runner: "codex",
        availability: { available: true },
        degradedReason: "test-only injected command provider",
        capability: {
          protocol: "exec-jsonl",
          bidirectional: false,
          sessions: false,
          permissions: false,
          cancellation: "process",
          finalResponseCapture: "jsonl-fallback",
          telemetry: {},
        },
      };
    },
    async createSession(input) {
      return new CommandSession(input, commands);
    },
  };
}

class CommandSession implements AgentCommunicationSession {
  readonly nativeSessionId = undefined;
  readonly events: AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  >;
  private submitted = false;

  constructor(
    private readonly input: CreateCommunicationSessionInput,
    private readonly commands: Readonly<Record<string, TestRoleCommand>>,
  ) {
    this.events = this.stream();
  }

  async start(): Promise<void> {}
  async submit(_assignment: AgentAssignmentV1): Promise<void> {
    this.submitted = true;
  }
  async respondToPermission(_response: PermissionResponse): Promise<void> {}
  async cancel(): Promise<void> {}
  async close(): Promise<void> {}

  private async *stream(): AsyncGenerator<
    ConduitRuntimeEvent,
    NativeTerminalResult,
    void
  > {
    if (!this.submitted)
      return { status: "failed", diagnostics: ["not submitted"] };
    const planned = this.commands[this.input.assignment.role];
    if (!planned)
      return { status: "failed", diagnostics: ["missing test command"] };
    try {
      const result = await executeFile(planned.command, [...planned.args], {
        cwd: this.input.workspaceRoot,
        maxBuffer: 1_000_000,
      });
      const candidate = result.stdout
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line.startsWith("{") && line.endsWith("}"))
        .at(-1);
      if (candidate) {
        yield {
          version: "1.0",
          sequence: 1,
          receivedAt: new Date().toISOString(),
          context: {
            runId: this.input.assignment.assignmentId.split(":")[0],
            roleId: this.input.assignment.role,
          },
          provenance: "runner-reported",
          type: "final-response-candidate",
          payload: { message: "test response" },
        };
      }
      return {
        status: "completed",
        exitCode: 0,
        finalResponseCandidate: candidate,
        diagnostics: [],
      };
    } catch (error) {
      const failure = error as Error & { code?: number; stdout?: string };
      return {
        status: "failed",
        exitCode: typeof failure.code === "number" ? failure.code : 1,
        diagnostics: [failure.message],
      };
    }
  }
}
