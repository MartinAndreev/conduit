import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import type { WorktreeLifecycleRepository } from "../interfaces/worktree-lifecycle-repository.js";
import type { WorktreeLifecycleRecord } from "../types/worktree-lifecycle.js";

export class FileWorktreeLifecycleRepository implements WorktreeLifecycleRepository {
  constructor(private readonly stateDirectory: string) {}

  async save(record: WorktreeLifecycleRecord): Promise<void> {
    const file = this.recordPath(record.runId);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, `${JSON.stringify(record, null, 2)}\n`);
  }

  async listExpired(cutoff: Date): Promise<readonly WorktreeLifecycleRecord[]> {
    const directory = this.metadataDirectory();
    const entries = await readdir(directory, { withFileTypes: true }).catch(
      () => [],
    );
    const records = await Promise.all(
      entries
        .filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
        .map(async (entry) => {
          return readFile(path.join(directory, entry.name), "utf8")
            .then((content) => JSON.parse(content) as WorktreeLifecycleRecord)
            .catch(() => undefined);
        }),
    );
    return records.filter((record): record is WorktreeLifecycleRecord => {
      return Boolean(
        record &&
        Number.isFinite(new Date(record.completedAt).getTime()) &&
        new Date(record.completedAt).getTime() <= cutoff.getTime(),
      );
    });
  }

  async remove(runId: string): Promise<void> {
    await rm(this.recordPath(runId), { force: true });
  }

  private metadataDirectory(): string {
    return path.join(this.stateDirectory, "worktree-metadata");
  }

  private recordPath(runId: string): string {
    return path.join(this.metadataDirectory(), `${runId}.json`);
  }
}
