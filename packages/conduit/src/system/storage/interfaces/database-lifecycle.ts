import type {
  DatabaseConnection,
  DatabaseStatement,
  ShutdownHook,
} from "./database.js";

export interface DatabaseLifecycle {
  registerStatement(statement: DatabaseStatement): void;
  registerConnection(connection: DatabaseConnection): void;
  registerHook(hook: ShutdownHook): void;
  shutdown(): Promise<readonly string[]>;
}
