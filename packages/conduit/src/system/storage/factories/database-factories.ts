import { chmod, mkdir } from "node:fs/promises";
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
import { createDefaultMigrationRegistry } from "../migrations/default-registry.js";
import { DefaultMigrationRunner } from "../migrations/migration-runner.js";

export class ProjectDatabaseFactory implements DatabaseFactory {
  constructor(
    private readonly projectRoot: string,
    private readonly lockFactory: ProjectLockFactory = new FileProjectLockFactory(),
    private readonly stateDirectory?: string,
  ) {}

  async open(): Promise<DatabaseConnection> {
    return this.openOwned(true);
  }

  async openWithoutMigrations(): Promise<DatabaseConnection> {
    return this.openOwned(false);
  }

  private async openOwned(runMigrations: boolean): Promise<DatabaseConnection> {
    const paths = resolveProjectDatabasePaths(
      this.projectRoot,
      this.stateDirectory,
    );
    await ensureConduitStateGitIgnored(paths.directory);
    const lock = await this.lockFactory.acquire(
      this.projectRoot,
      paths.directory,
    );
    try {
      await mkdir(paths.directory, { recursive: true, mode: 0o700 });
      const connection = await openEmbeddedTursoConnection(
        "project",
        paths.databasePath,
      );
      if (runMigrations)
        await new DefaultMigrationRunner(
          createDefaultMigrationRegistry(),
        ).migrate(connection, "project");
      await chmod(paths.databasePath, 0o600);
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
  backup: DatabaseConnection["backup"] = (destinationPath) =>
    this.connection.backup(destinationPath);
  checkpoint: DatabaseConnection["checkpoint"] = () =>
    this.connection.checkpoint();

  async close(): Promise<void> {
    try {
      await this.connection.close();
    } finally {
      await this.lock.release();
    }
  }
}

export class GlobalDatabaseFactory implements DatabaseFactory {
  constructor(private readonly environment: NodeJS.ProcessEnv = process.env) {}

  async open(): Promise<DatabaseConnection> {
    return this.openOwned(true);
  }

  async openWithoutMigrations(): Promise<DatabaseConnection> {
    return this.openOwned(false);
  }

  private async openOwned(runMigrations: boolean): Promise<DatabaseConnection> {
    const paths = resolveGlobalDatabasePaths(this.environment);
    await mkdir(paths.directory, { recursive: true, mode: 0o700 });
    await chmod(paths.directory, 0o700);
    const connection = await openEmbeddedTursoConnection(
      "global",
      paths.databasePath,
    );
    try {
      if (runMigrations)
        await new DefaultMigrationRunner(
          createDefaultMigrationRegistry(),
        ).migrate(connection, "global");
      await chmod(paths.databasePath, 0o600);
      return connection;
    } catch (error) {
      await connection.close();
      throw error;
    }
  }
}
