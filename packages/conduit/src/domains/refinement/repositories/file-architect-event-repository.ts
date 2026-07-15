import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import type { ArchitectEventRepository } from "../interfaces/architect-event-repository.js";
import { extractArchitectEvents } from "../helpers/architect-event-parser.js";
import type { ArchitectEvent } from "../types/architect-event.js";

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

    const modifiedAt = await stat(logFile)
      .then((entry) => entry.mtime.toISOString())
      .catch(() => new Date().toISOString());
    return extractArchitectEvents(transcript, modifiedAt);
  }
}
