import { mkdir } from "node:fs/promises";
import type { DatabaseConnection } from "../interfaces/database.js";
import type { DatabaseFactory } from "../interfaces/factory.js";
import type {
  ProjectLock,
  ProjectLockFactory,
} from "../interfaces/project-lock.js";
import { FileProjectLockFactory } from "../repositories/project-lock.js";
import { openEmbeddedTursoConnection } from "../adapters/embedded-turso.js";
import { ensureConduitStateGitIgnored } from "./gitignore.js";
import {
  resolveGlobalDatabasePaths,
  resolveProjectDatabasePaths,
} from "./path-resolution.js";

export class ProjectDatabaseFactory implements DatabaseFactory {
  constructor(
    private readonly projectRoot: string,
    private readonly lockFactory: ProjectLockFactory = new FileProjectLockFactory(),
    private readonly stateDirectory?: string,
  ) {}

  async open(): Promise<DatabaseConnection> {
    const paths = resolveProjectDatabasePaths(
      this.projectRoot,
      this.stateDirectory,
    );
    await ensureConduitStateGitIgnored(this.projectRoot);
    const lock = await this.lockFactory.acquire(this.projectRoot);
    try {
      await mkdir(paths.directory, { recursive: true });
      const connection = await openEmbeddedTursoConnection(
        "project",
        paths.databasePath,
      );
      return new LockedProjectDatabaseConnection(connection, lock);
    } catch (error) {
      await lock.release();
      throw error;
    }
  }
}

class LockedProjectDatabaseConnection implements DatabaseConnection {
  readonly databasePath: string;

  constructor(
    private readonly connection: DatabaseConnection,
    private readonly lock: ProjectLock,
  ) {
    this.databasePath = connection.databasePath;
  }

  execute: DatabaseConnection["execute"] = (sql, parameters) =>
    this.connection.execute(sql, parameters);
  prepare: DatabaseConnection["prepare"] = (sql) =>
    this.connection.prepare(sql);

  async close(): Promise<void> {
    try {
      await this.connection.close();
    } finally {
      await this.lock.release();
    }
  }
}

export class GlobalDatabaseFactory implements DatabaseFactory {
  async open(): Promise<DatabaseConnection> {
    const paths = resolveGlobalDatabasePaths();
    await mkdir(paths.directory, { recursive: true, mode: 0o700 });
    return openEmbeddedTursoConnection("global", paths.databasePath);
  }
}
