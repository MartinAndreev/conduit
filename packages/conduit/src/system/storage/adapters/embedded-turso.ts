import type {
  DatabaseConnection,
  DatabaseStatement,
} from "../interfaces/database.js";
import type {
  QueryResult,
  QueryResultRow,
  SqlParameters,
} from "../types/database.js";
import { toStorageError } from "../errors/storage-error.js";
import type { DatabaseScope } from "../types/database.js";

type TursoQueryResult = Readonly<{
  rows?: readonly QueryResultRow[];
  rowsAffected?: number;
  changes?: number;
  lastInsertRowid?: bigint | number;
  lastInsertRowId?: bigint | number;
}>;

type TursoStatementLike = {
  execute?: (
    parameters?: SqlParameters,
  ) => Promise<TursoQueryResult> | TursoQueryResult;
  all?: (
    parameters?: SqlParameters,
  ) =>
    | Promise<readonly QueryResultRow[] | TursoQueryResult>
    | readonly QueryResultRow[]
    | TursoQueryResult;
  get?: (
    parameters?: SqlParameters,
  ) => Promise<QueryResultRow | undefined> | QueryResultRow | undefined;
  finalize?: () => Promise<void> | void;
};

type TursoDatabaseLike = {
  execute?: (
    sql: string,
    parameters?: SqlParameters,
  ) => Promise<TursoQueryResult> | TursoQueryResult;
  prepare?: (sql: string) => Promise<TursoStatementLike> | TursoStatementLike;
  close?: () => Promise<void> | void;
};

function isRowArray(
  value: TursoQueryResult | readonly QueryResultRow[] | undefined,
): value is readonly QueryResultRow[] {
  return Array.isArray(value);
}

function normalizeResult(
  value: TursoQueryResult | readonly QueryResultRow[] | undefined,
): QueryResult {
  if (isRowArray(value)) {
    return { rows: value, rowsAffected: 0 };
  }
  return {
    rows: value?.rows ?? [],
    rowsAffected: value?.rowsAffected ?? value?.changes ?? 0,
    lastInsertRowid:
      typeof value?.lastInsertRowid === "number"
        ? BigInt(value.lastInsertRowid)
        : value?.lastInsertRowid,
  };
}

export class EmbeddedTursoStatement implements DatabaseStatement {
  constructor(private readonly statement: TursoStatementLike) {}

  async execute(parameters?: SqlParameters): Promise<QueryResult> {
    if (this.statement.execute)
      return normalizeResult(await this.statement.execute(parameters));
    if (this.statement.all)
      return normalizeResult(await this.statement.all(parameters));
    throw new Error("Turso statement does not support execute");
  }

  async all(parameters?: SqlParameters): Promise<QueryResult> {
    if (this.statement.all)
      return normalizeResult(await this.statement.all(parameters));
    return this.execute(parameters);
  }

  async get(parameters?: SqlParameters): Promise<QueryResultRow | undefined> {
    if (this.statement.get) return this.statement.get(parameters);
    return (await this.all(parameters)).rows[0];
  }

  async finalize(): Promise<void> {
    await this.statement.finalize?.();
  }
}

export class EmbeddedTursoConnection implements DatabaseConnection {
  constructor(
    readonly databasePath: string,
    private readonly database: TursoDatabaseLike,
  ) {}

  async execute(sql: string, parameters?: SqlParameters): Promise<QueryResult> {
    if (!this.database.execute)
      throw new Error("Turso database does not support execute");
    return normalizeResult(await this.database.execute(sql, parameters));
  }

  async prepare(sql: string): Promise<DatabaseStatement> {
    if (!this.database.prepare)
      throw new Error("Turso database does not support prepare");
    return new EmbeddedTursoStatement(await this.database.prepare(sql));
  }

  async close(): Promise<void> {
    await this.database.close?.();
  }
}

export async function openEmbeddedTursoConnection(
  scope: DatabaseScope,
  databasePath: string,
): Promise<DatabaseConnection> {
  try {
    const module = await import("@tursodatabase/database");
    const loaded = module as unknown as {
      connect?: (path: string) => unknown | Promise<unknown>;
      open?: (path: string) => unknown | Promise<unknown>;
      Database: new (path: string) => unknown;
    };
    const factory = loaded.connect ?? loaded.open;
    const database = factory
      ? await factory(databasePath)
      : new loaded.Database(databasePath);
    return new EmbeddedTursoConnection(
      databasePath,
      database as TursoDatabaseLike,
    );
  } catch (error) {
    throw toStorageError({
      scope,
      operation: "open embedded Turso database",
      cause: error,
      remediation:
        "Install @tursodatabase/database and verify the native binding supports this platform.",
    });
  }
}
