import { mkdir, open, readFile, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import type {
  ProjectLock,
  ProjectLockFactory,
} from "../interfaces/project-lock.js";
import { StorageError } from "../errors/storage-error.js";

export class FileProjectLock implements ProjectLock {
  constructor(
    readonly lockPath: string,
    private readonly ownership: string,
  ) {}

  async release(): Promise<void> {
    const current = await readFile(this.lockPath, "utf8").catch(() => "");
    if (current === this.ownership) await rm(this.lockPath, { force: true });
  }
}

export class FileProjectLockFactory implements ProjectLockFactory {
  async acquire(
    projectRoot: string,
    stateDirectory = join(projectRoot, ".conduit"),
  ): Promise<ProjectLock> {
    const lockPath = join(stateDirectory, "state.db.lock");
    await mkdir(dirname(lockPath), { recursive: true });
    const ownership = `${process.pid}\n${new Date().toISOString()}\n`;
    try {
      const handle = await open(lockPath, "wx");
      await handle.writeFile(ownership, "utf8");
      await handle.close();
      return new FileProjectLock(lockPath, ownership);
    } catch (error) {
      throw new StorageError({
        scope: "project",
        operation: "acquire project database lock",
        message: `project database is already owned by another Conduit process`,
        remediation: `Close the other Conduit process or remove ${lockPath} only after confirming it is stale.`,
        cause: error,
      });
    }
  }
}
