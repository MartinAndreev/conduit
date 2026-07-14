import type { QueryResult, SqlParameters } from "../types/database.js";

export interface DatabaseStatement {
  execute(parameters?: SqlParameters): Promise<QueryResult>;
  all(parameters?: SqlParameters): Promise<QueryResult>;
  get(
    parameters?: SqlParameters,
  ): Promise<Readonly<Record<string, unknown>> | undefined>;
  finalize(): Promise<void>;
}

export interface DatabaseConnection {
  readonly databasePath: string;
  execute(sql: string, parameters?: SqlParameters): Promise<QueryResult>;
  prepare(sql: string): Promise<DatabaseStatement>;
  backup(destinationPath: string): Promise<void>;
  checkpoint(): Promise<void>;
  close(): Promise<void>;
}

export interface TransactionRunner {
  transaction<T>(operation: () => Promise<T>): Promise<T>;
}

export interface BatchWriter<T> {
  writeBatch(items: readonly T[]): Promise<void>;
}

export interface ShutdownHook {
  close(): Promise<void>;
}
