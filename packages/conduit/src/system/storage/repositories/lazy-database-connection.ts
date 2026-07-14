import type {
  DatabaseConnection,
  DatabaseStatement,
} from "../interfaces/database.js";
import type { DatabaseFactory } from "../interfaces/factory.js";
import type { QueryResult, SqlParameters } from "../types/database.js";

export class LazyDatabaseConnection implements DatabaseConnection {
  private connection?: DatabaseConnection;
  private opening?: Promise<DatabaseConnection>;
  private closing?: Promise<void>;
  private closed = false;
  private readonly activeOperations = new Set<Promise<unknown>>();

  constructor(
    private readonly factory: DatabaseFactory,
    readonly databasePath: string,
  ) {}

  private async getConnection(): Promise<DatabaseConnection> {
    if (this.closed)
      throw new Error("Database connection is closing or already closed.");
    if (this.connection) return this.connection;
    this.opening ??= this.factory.open();
    this.connection = await this.opening;
    return this.connection;
  }

  private track<T>(operation: Promise<T>): Promise<T> {
    const tracked = operation.finally(() => {
      this.activeOperations.delete(tracked);
    });
    this.activeOperations.add(tracked);
    return tracked;
  }

  async execute(sql: string, parameters?: SqlParameters): Promise<QueryResult> {
    return this.track(
      this.getConnection().then((connection) =>
        connection.execute(sql, parameters),
      ),
    );
  }

  async prepare(sql: string): Promise<DatabaseStatement> {
    return this.track(
      this.getConnection().then((connection) => connection.prepare(sql)),
    );
  }

  async backup(destinationPath: string): Promise<void> {
    await this.track(
      this.getConnection().then((connection) =>
        connection.backup(destinationPath),
      ),
    );
  }

  async checkpoint(): Promise<void> {
    const pendingConnection = this.connection
      ? Promise.resolve(this.connection)
      : this.opening;
    if (pendingConnection)
      await this.track(
        pendingConnection.then((connection) => connection.checkpoint()),
      );
  }

  async close(): Promise<void> {
    this.closed = true;
    this.closing ??= (async () => {
      await Promise.allSettled([...this.activeOperations]);
      const connection =
        this.connection ??
        (this.opening ? await this.opening.catch(() => undefined) : undefined);
      this.connection = undefined;
      this.opening = undefined;
      if (connection) await connection.close();
    })();
    await this.closing;
  }
}
