import { mkdir, open, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import type { ProjectLock, ProjectLockFactory } from "../interfaces/project-lock.js";
import { StorageError } from "../errors/storage-error.js";

export class FileProjectLock implements ProjectLock {
  constructor(readonly lockPath: string) {}

  async release(): Promise<void> {
    await rm(this.lockPath, { force: true });
  }
}

export class FileProjectLockFactory implements ProjectLockFactory {
  async acquire(projectRoot: string): Promise<ProjectLock> {
    const lockPath = join(projectRoot, ".conduit", "state.db.lock");
    await mkdir(dirname(lockPath), { recursive: true });
    try {
      const handle = await open(lockPath, "wx");
      await handle.writeFile(`${process.pid}\n${new Date().toISOString()}\n`, "utf8");
      await handle.close();
      return new FileProjectLock(lockPath);
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
