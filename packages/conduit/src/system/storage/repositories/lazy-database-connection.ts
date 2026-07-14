import type {
  DatabaseConnection,
  DatabaseStatement,
} from "../interfaces/database.js";
import type { DatabaseFactory } from "../interfaces/factory.js";
import type { QueryResult, SqlParameters } from "../types/database.js";

export class LazyDatabaseConnection implements DatabaseConnection {
  private connection?: DatabaseConnection;
  private opening?: Promise<DatabaseConnection>;

  constructor(
    private readonly factory: DatabaseFactory,
    readonly databasePath: string,
  ) {}

  private async getConnection(): Promise<DatabaseConnection> {
    if (this.connection) return this.connection;
    this.opening ??= this.factory.open();
    this.connection = await this.opening;
    return this.connection;
  }

  async execute(sql: string, parameters?: SqlParameters): Promise<QueryResult> {
    return (await this.getConnection()).execute(sql, parameters);
  }

  async prepare(sql: string): Promise<DatabaseStatement> {
    return (await this.getConnection()).prepare(sql);
  }

  async backup(destinationPath: string): Promise<void> {
    await (await this.getConnection()).backup(destinationPath);
  }

  async checkpoint(): Promise<void> {
    if (this.connection) await this.connection.checkpoint();
  }

  async close(): Promise<void> {
    if (!this.connection) return;
    const connection = this.connection;
    this.connection = undefined;
    this.opening = undefined;
    await connection.close();
  }
}
