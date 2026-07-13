import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import type { ArchitectEvent } from "../types/architect-event.js";
import type { ArchitectEventRepository } from "../interfaces/architect-event-repository.js";

function extractEventsFromTranscript(transcript: string): ArchitectEvent[] {
  const events: ArchitectEvent[] = [];
  const lines = transcript.split("\n");
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (line === "exec") {
      const command = lines[i + 1] ?? "";
      events.push({
        type: "tool-call",
        timestamp: new Date().toISOString(),
        content: command,
      });
      i += 2;
      continue;
    }

    if (line === "analysis" || line === "codex") {
      events.push({
        type: "activity",
        timestamp: new Date().toISOString(),
        content:
          line === "analysis"
            ? "Analyzing project context"
            : "Refining feature specification",
      });
      i += 1;
      continue;
    }

    if (line === "apply patch") {
      events.push({
        type: "activity",
        timestamp: new Date().toISOString(),
        content: "Applying specification patch",
      });
      i += 1;
      continue;
    }

    if (line === "patch: completed") {
      events.push({
        type: "lifecycle",
        timestamp: new Date().toISOString(),
        content: "Patch completed",
      });
      i += 1;
      continue;
    }

    if (line.startsWith("diff --git ")) {
      const diffLines: string[] = [line];
      i += 1;
      while (
        i < lines.length &&
        !lines[i].match(/^(?:exec|analysis|codex|apply patch|patch: completed)/)
      ) {
        diffLines.push(lines[i]);
        i += 1;
      }
      const diff = diffLines.join("\n");
      const files = [
        ...new Set(
          [...diff.matchAll(/^diff --git a\/(.+?) b\//gm)].map((m) => m[1]),
        ),
      ];
      events.push({
        type: "patch",
        timestamp: new Date().toISOString(),
        content: `Applied patch: ${files.length} file${files.length === 1 ? "" : "s"}`,
        files,
        diff,
      });
      continue;
    }

    i += 1;
  }

  return events;
}

function deduplicatePatchEvents(events: ArchitectEvent[]): ArchitectEvent[] {
  const seen = new Map<string, ArchitectEvent>();
  const result: ArchitectEvent[] = [];

  for (const event of events) {
    if (event.type === "patch" && event.files) {
      const key = [...event.files].sort().join(",");
      if (seen.has(key)) {
        continue;
      }
      seen.set(key, event);
    }
    result.push(event);
  }

  return result;
}

export class FileArchitectEventRepository implements ArchitectEventRepository {
  private readonly projectRoot: string;
  private readonly stateDir: string;

  constructor(projectRoot: string, stateDir: string = ".conduit") {
    this.projectRoot = projectRoot;
    this.stateDir = stateDir;
  }

  async loadEvents(featureId: string): Promise<readonly ArchitectEvent[]> {
    const runsDir = path.join(this.projectRoot, this.stateDir, "runs");
    const entries = await readdir(runsDir, { withFileTypes: true }).catch(
      () => [] as import("node:fs").Dirent[],
    );

    const matchingRuns = entries
      .filter(
        (e) => e.isDirectory() && e.name.startsWith(`refine-${featureId}-`),
      )
      .sort((a, b) => b.name.localeCompare(a.name));

    if (matchingRuns.length === 0) {
      return [];
    }

    const latestRun = matchingRuns[0];
    const logFile = path.join(runsDir, latestRun.name, "architect.log");
    const transcript = await readFile(logFile, "utf8").catch(() => "");

    if (!transcript.trim()) {
      return [];
    }

    const events = extractEventsFromTranscript(transcript);
    return deduplicatePatchEvents(events);
  }
}
