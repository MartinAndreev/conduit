export type DatabaseScope = "project" | "global";

export type SqlParameter = string | number | bigint | boolean | null | Uint8Array;

export type SqlParameters = readonly SqlParameter[] | Readonly<Record<string, SqlParameter>>;

export type QueryResultRow = Readonly<Record<string, unknown>>;

export type QueryResult = Readonly<{
  rows: readonly QueryResultRow[];
  rowsAffected: number;
  lastInsertRowid?: bigint;
}>;

export type DatabasePathSet = Readonly<{
  directory: string;
  databasePath: string;
}>;
