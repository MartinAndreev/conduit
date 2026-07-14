import type { DatabaseConnection } from "./database.js";

export interface DatabaseFactory {
  open(): Promise<DatabaseConnection>;
}
