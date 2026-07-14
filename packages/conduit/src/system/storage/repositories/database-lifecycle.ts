import type { DatabaseLifecycle } from "../interfaces/database-lifecycle.js";
import type {
  DatabaseConnection,
  DatabaseStatement,
  ShutdownHook,
} from "../interfaces/database.js";
import { redactStorageDiagnostic } from "../errors/storage-error.js";

export class DefaultDatabaseLifecycle implements DatabaseLifecycle {
  private readonly statements: DatabaseStatement[] = [];
  private readonly connections: DatabaseConnection[] = [];
  private readonly hooks: ShutdownHook[] = [];
  private shutdownPromise?: Promise<readonly string[]>;

  constructor(
    private readonly emitDiagnostic: (
      diagnostic: string,
    ) => void = console.error,
  ) {}

  registerStatement(statement: DatabaseStatement): void {
    this.statements.push(statement);
  }

  registerConnection(connection: DatabaseConnection): void {
    this.connections.push(connection);
  }

  registerHook(hook: ShutdownHook): void {
    this.hooks.push(hook);
  }

  shutdown(): Promise<readonly string[]> {
    this.shutdownPromise ??= this.performShutdown();
    return this.shutdownPromise;
  }

  private async performShutdown(): Promise<readonly string[]> {
    const diagnostics: string[] = [];
    const attempt = async (label: string, operation: () => Promise<void>) => {
      try {
        await operation();
      } catch (error) {
        const detail = redactStorageDiagnostic(
          error instanceof Error ? error.message : String(error),
        );
        const diagnostic = `${label}: ${detail}`;
        diagnostics.push(diagnostic);
        this.emitDiagnostic(diagnostic);
      }
    };

    for (const statement of this.statements.splice(0).reverse())
      await attempt("Could not finalize database statement", () =>
        statement.finalize(),
      );
    for (const connection of this.connections.splice(0).reverse()) {
      await attempt("Could not checkpoint database", () =>
        connection.checkpoint(),
      );
      await attempt("Could not close database", () => connection.close());
    }
    for (const hook of this.hooks.splice(0).reverse())
      await attempt("Could not complete shutdown hook", () => hook.close());
    return diagnostics;
  }
}
