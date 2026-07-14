import type { DatabaseScope } from "../types/database.js";
import { redactSecrets } from "../security/secret-redaction.js";

export class StorageError extends Error {
  readonly scope: DatabaseScope;
  readonly operation: string;
  readonly remediation: string;

  constructor(input: {
    scope: DatabaseScope;
    operation: string;
    message: string;
    remediation: string;
    cause?: unknown;
  }) {
    super(input.message, { cause: input.cause });
    this.name = "StorageError";
    this.scope = input.scope;
    this.operation = input.operation;
    this.remediation = input.remediation;
  }
}

export function redactStorageDiagnostic(value: unknown): string {
  const text = value instanceof Error ? value.message : String(value);
  return redactSecrets(text);
}

export function toStorageError(input: {
  scope: DatabaseScope;
  operation: string;
  cause: unknown;
  remediation: string;
}): StorageError {
  return new StorageError({
    scope: input.scope,
    operation: input.operation,
    message: `${input.operation} failed for ${input.scope} database: ${redactStorageDiagnostic(input.cause)}`,
    remediation: input.remediation,
    cause: new Error(redactStorageDiagnostic(input.cause)),
  });
}
