import type { Result } from "../result.js";

export interface Query {
  readonly type: string;
}

export interface ApplicationError {
  readonly code: string;
  readonly message: string;
  readonly cause?: unknown;
}

export type QueryHandler<TQuery extends Query = Query, TReadModel = unknown> = (
  query: TQuery,
) => Promise<Result<TReadModel, ApplicationError>>;

export class QueryBus {
  private readonly handlers = new Map<string, QueryHandler>();

  register<TQuery extends Query, TReadModel>(
    type: TQuery["type"],
    handler: QueryHandler<TQuery, TReadModel>,
  ): void {
    if (this.handlers.has(type)) {
      throw new Error(`Duplicate query handler registration: ${type}`);
    }
    this.handlers.set(type, handler as QueryHandler);
  }

  async execute<TQuery extends Query, TReadModel>(
    query: TQuery,
  ): Promise<Result<TReadModel, ApplicationError>> {
    const handler = this.handlers.get(query.type);
    if (!handler) {
      return {
        success: false,
        error: {
          code: "HANDLER_NOT_FOUND",
          message: `No handler registered for query: ${query.type}`,
        },
      };
    }
    try {
      const result = await handler(query);
      return Object.freeze(result) as Result<TReadModel, ApplicationError>;
    } catch (thrown) {
      const error =
        thrown instanceof Error ? thrown : new Error(String(thrown));
      return {
        success: false,
        error: {
          code: "HANDLER_ERROR",
          message: error.message,
          cause: error,
        },
      };
    }
  }
}
