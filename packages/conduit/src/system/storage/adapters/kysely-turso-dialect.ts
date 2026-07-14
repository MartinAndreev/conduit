import {
  Kysely,
  SqliteAdapter,
  SqliteIntrospector,
  SqliteQueryCompiler,
} from "kysely";
import type {
  CompiledQuery,
  DatabaseConnection as KyselyDatabaseConnection,
  Dialect,
  Driver,
  Kysely as KyselyDatabase,
  QueryResult as KyselyQueryResult,
  TransactionSettings,
} from "kysely";
import type { DatabaseConnection } from "../interfaces/database.js";
import type { SqlParameter } from "../types/database.js";

function toSqlParameter(value: unknown): SqlParameter {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "bigint" ||
    typeof value === "boolean" ||
    value instanceof Uint8Array
  ) {
    return value;
  }
  if (value instanceof Date) return value.toISOString();
  throw new TypeError(`Unsupported SQL parameter type: ${typeof value}`);
}

class TursoKyselyConnection implements KyselyDatabaseConnection {
  constructor(private readonly connection: DatabaseConnection) {}

  async executeQuery<R>(
    compiledQuery: CompiledQuery,
  ): Promise<KyselyQueryResult<R>> {
    const statement = await this.connection.prepare(compiledQuery.sql);
    try {
      const result = await statement.execute(
        compiledQuery.parameters.map(toSqlParameter),
      );
      return {
        rows: result.rows as R[],
        numAffectedRows: BigInt(result.rowsAffected),
        ...(result.lastInsertRowid === undefined
          ? {}
          : { insertId: result.lastInsertRowid }),
      };
    } finally {
      await statement.finalize();
    }
  }

  async *streamQuery<R>(
    compiledQuery: CompiledQuery,
  ): AsyncIterableIterator<KyselyQueryResult<R>> {
    yield await this.executeQuery<R>(compiledQuery);
  }
}

class TursoKyselyDriver implements Driver {
  private readonly kyselyConnection: KyselyDatabaseConnection;

  constructor(private readonly connection: DatabaseConnection) {
    this.kyselyConnection = new TursoKyselyConnection(connection);
  }

  async init(): Promise<void> {}

  async acquireConnection(): Promise<KyselyDatabaseConnection> {
    return this.kyselyConnection;
  }

  async beginTransaction(
    _connection: KyselyDatabaseConnection,
    _settings: TransactionSettings,
  ): Promise<void> {
    await this.connection.execute("BEGIN IMMEDIATE");
  }

  async commitTransaction(): Promise<void> {
    await this.connection.execute("COMMIT");
  }

  async rollbackTransaction(): Promise<void> {
    await this.connection.execute("ROLLBACK");
  }

  async releaseConnection(): Promise<void> {}

  async destroy(): Promise<void> {}
}

export class TursoKyselyDialect implements Dialect {
  constructor(private readonly connection: DatabaseConnection) {}

  createDriver(): Driver {
    return new TursoKyselyDriver(this.connection);
  }

  createQueryCompiler(): SqliteQueryCompiler {
    return new SqliteQueryCompiler();
  }

  createAdapter(): SqliteAdapter {
    return new SqliteAdapter();
  }

  createIntrospector(db: KyselyDatabase<unknown>): SqliteIntrospector {
    return new SqliteIntrospector(db);
  }
}

export function createTursoKysely<Database>(
  connection: DatabaseConnection,
): Kysely<Database> {
  return new Kysely<Database>({
    dialect: new TursoKyselyDialect(connection),
  });
}
