import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  agentResponseMcpCaptureEnvironmentKey,
  agentResponseMcpReadyEnvironmentKey,
  agentResponseMcpToolName,
} from "./agent-response-mcp-server.js";

const sensitiveEnvironmentKey =
  /(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|AUTH|DATABASE|TURSO|LIBSQL|CONDUIT_DB)/i;

export function createAgentResponseToolRuntime() {
  const directory = mkdtempSync(path.join(tmpdir(), "conduit-agent-response-"));
  const capturePath = path.join(directory, "response.json");
  const readyPath = path.join(directory, "ready");
  const entry = process.argv[1] ? path.resolve(process.argv[1]) : undefined;
  const executable = path.resolve(process.execPath);
  const bundledExecutable = entry?.startsWith("/$bunfs/") === true;
  const tsconfigPath = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../../..",
    "tsconfig.json",
  );
  const args = [
    ...process.execArgv,
    ...(entry && entry !== executable && !bundledExecutable ? [entry] : []),
    "__agent-response-mcp",
  ];
  return {
    toolName: agentResponseMcpToolName,
    mcpServer: {
      name: "conduit",
      command: process.execPath,
      args,
      env: [
        {
          name: agentResponseMcpCaptureEnvironmentKey,
          value: capturePath,
        },
        {
          name: agentResponseMcpReadyEnvironmentKey,
          value: readyPath,
        },
        ...(existsSync(tsconfigPath)
          ? [{ name: "TSX_TSCONFIG_PATH", value: tsconfigPath }]
          : []),
        ...Object.keys(process.env)
          .filter(
            (name) =>
              name !== agentResponseMcpCaptureEnvironmentKey &&
              sensitiveEnvironmentKey.test(name),
          )
          .map((name) => ({ name, value: "" })),
      ],
    },
    ready(): boolean {
      return existsSync(readyPath);
    },
    read(): string | undefined {
      try {
        return readFileSync(capturePath, "utf8").trim() || undefined;
      } catch {
        return undefined;
      }
    },
    cleanup(): void {
      rmSync(directory, { recursive: true, force: true });
    },
  };
}
