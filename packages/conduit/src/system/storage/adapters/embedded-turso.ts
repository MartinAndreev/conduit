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
  readonly reader?: boolean;
  run: (
    parameters?: SqlParameters,
  ) => Promise<TursoQueryResult> | TursoQueryResult;
  all: (
    parameters?: SqlParameters,
  ) =>
    | Promise<readonly QueryResultRow[] | TursoQueryResult>
    | readonly QueryResultRow[]
    | TursoQueryResult;
  get: (
    parameters?: SqlParameters,
  ) => Promise<QueryResultRow | undefined> | QueryResultRow | undefined;
  close: () => Promise<void> | void;
};

type TursoDatabaseLike = {
  exec: (sql: string) => Promise<void> | void;
  prepare: (sql: string) => Promise<TursoStatementLike> | TursoStatementLike;
  close: () => Promise<void> | void;
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
  private finalized = false;

  constructor(
    private readonly statement: TursoStatementLike,
    private readonly onFinalize: () => void = () => {},
  ) {}

  async execute(parameters?: SqlParameters): Promise<QueryResult> {
    if (this.statement.reader)
      return normalizeResult(await this.statement.all(parameters));
    return normalizeResult(await this.statement.run(parameters));
  }

  async all(parameters?: SqlParameters): Promise<QueryResult> {
    return normalizeResult(await this.statement.all(parameters));
  }

  async get(parameters?: SqlParameters): Promise<QueryResultRow | undefined> {
    return this.statement.get(parameters);
  }

  async finalize(): Promise<void> {
    if (this.finalized) return;
    this.finalized = true;
    try {
      await this.statement.close();
    } finally {
      this.onFinalize();
    }
  }
}

export class EmbeddedTursoConnection implements DatabaseConnection {
  private readonly statements = new Set<EmbeddedTursoStatement>();

  constructor(
    readonly databasePath: string,
    private readonly database: TursoDatabaseLike,
  ) {}

  async execute(sql: string, parameters?: SqlParameters): Promise<QueryResult> {
    if (parameters) {
      const statement = await this.prepare(sql);
      try {
        return await statement.execute(parameters);
      } finally {
        await statement.finalize();
      }
    }
    await this.database.exec(sql);
    return { rows: [], rowsAffected: 0 };
  }

  async prepare(sql: string): Promise<DatabaseStatement> {
    const statement = new EmbeddedTursoStatement(
      await this.database.prepare(sql),
      () => this.statements.delete(statement),
    );
    this.statements.add(statement);
    return statement;
  }

  async backup(destinationPath: string): Promise<void> {
    const escaped = destinationPath.replaceAll("'", "''");
    await this.database.exec(`VACUUM INTO '${escaped}'`);
  }

  async checkpoint(): Promise<void> {
    await this.database.exec("PRAGMA wal_checkpoint(TRUNCATE)");
  }

  async close(): Promise<void> {
    try {
      await Promise.all(
        [...this.statements].map((statement) => statement.finalize()),
      );
      await this.checkpoint();
    } finally {
      await this.database.close();
    }
  }
}

export async function openEmbeddedTursoConnection(
  scope: DatabaseScope,
  databasePath: string,
): Promise<DatabaseConnection> {
  try {
    const module = await import("@tursodatabase/database");
    const database = await module.connect(databasePath);
    return new EmbeddedTursoConnection(
      databasePath,
      database as TursoDatabaseLike,
    );
  } catch (error) {
    const diagnostic = error instanceof Error ? error.message : String(error);
    const storageDamage =
      /(?:not a database|malformed|corrupt|short read|I\/O error)/i.test(
        diagnostic,
      );
    throw toStorageError({
      scope,
      operation: "open embedded Turso database",
      cause: error,
      remediation: storageDamage
        ? "Stop Conduit, preserve the damaged file, and restore the newest validated backup before retrying."
        : "Install @tursodatabase/database and verify the native binding supports this platform.",
    });
  }
}
