import type {
  DatabaseConnection,
  TransactionRunner,
} from "../interfaces/database.js";

export class DatabaseTransactionRunner implements TransactionRunner {
  private pending = Promise.resolve();

  constructor(private readonly connection: DatabaseConnection) {}

  async transaction<T>(operation: () => Promise<T>): Promise<T> {
    const previous = this.pending;
    let release: (() => void) | undefined;
    this.pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    await previous;
    await this.connection.execute("BEGIN IMMEDIATE");
    try {
      const result = await operation();
      await this.connection.execute("COMMIT");
      return result;
    } catch (error) {
      await this.connection.execute("ROLLBACK");
      throw error;
    } finally {
      release?.();
    }
  }
}

export class BoundedBatchWriter<T> {
  constructor(
    private readonly writeOne: (item: T) => Promise<void>,
    private readonly maxBatchSize = 100,
  ) {}

  async writeBatch(items: readonly T[]): Promise<void> {
    if (items.length > this.maxBatchSize) {
      throw new Error(
        `batch size ${items.length} exceeds limit ${this.maxBatchSize}`,
      );
    }
    for (const item of items) await this.writeOne(item);
  }
}
