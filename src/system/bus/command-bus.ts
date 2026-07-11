import type { Result } from "../result.js";

export interface Command {
  readonly type: string;
}

export interface ApplicationError {
  readonly code: string;
  readonly message: string;
  readonly cause?: unknown;
}

export type CommandHandler<
  TCommand extends Command = Command,
  TResult = unknown,
> = (command: TCommand) => Promise<Result<TResult, ApplicationError>>;

export class CommandBus {
  private readonly handlers = new Map<string, CommandHandler>();

  register<TCommand extends Command, TResult>(
    type: TCommand["type"],
    handler: CommandHandler<TCommand, TResult>,
  ): void {
    if (this.handlers.has(type)) {
      throw new Error(`Duplicate command handler registration: ${type}`);
    }
    this.handlers.set(type, handler as CommandHandler);
  }

  async dispatch<TCommand extends Command, TResult>(
    command: TCommand,
  ): Promise<Result<TResult, ApplicationError>> {
    const handler = this.handlers.get(command.type);
    if (!handler) {
      return {
        success: false,
        error: {
          code: "HANDLER_NOT_FOUND",
          message: `No handler registered for command: ${command.type}`,
        },
      };
    }
    try {
      return (await handler(command)) as Result<TResult, ApplicationError>;
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
