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
      await this.createLockFile(lockPath, ownership);
      return new FileProjectLock(lockPath, ownership);
    } catch (error) {
      let cause = error;
      if (
        (error as NodeJS.ErrnoException).code === "EEXIST" &&
        (await this.removeStaleLock(lockPath))
      ) {
        try {
          await this.createLockFile(lockPath, ownership);
          return new FileProjectLock(lockPath, ownership);
        } catch (retryError) {
          cause = retryError;
        }
      }
      throw new StorageError({
        scope: "project",
        operation: "acquire project database lock",
        message: `project database is already owned by another Conduit process`,
        remediation: `Close the other Conduit process or remove ${lockPath} only after confirming it is stale.`,
        cause,
      });
    }
  }

  private async createLockFile(
    lockPath: string,
    ownership: string,
  ): Promise<void> {
    const handle = await open(lockPath, "wx");
    try {
      await handle.writeFile(ownership, "utf8");
    } finally {
      await handle.close();
    }
  }

  private async removeStaleLock(lockPath: string): Promise<boolean> {
    const ownership = await readFile(lockPath, "utf8").catch(() => undefined);
    if (!ownership) return false;
    const ownerPid = Number.parseInt(ownership.split("\n", 1)[0] ?? "", 10);
    if (!Number.isSafeInteger(ownerPid) || ownerPid <= 0) return false;

    try {
      process.kill(ownerPid, 0);
      return false;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ESRCH") return false;
    }

    const unchanged =
      (await readFile(lockPath, "utf8").catch(() => "")) === ownership;
    if (!unchanged) return false;
    await rm(lockPath, { force: true });
    return true;
  }
}
