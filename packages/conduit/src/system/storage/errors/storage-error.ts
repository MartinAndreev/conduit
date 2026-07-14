import type { DatabaseScope } from "../types/database.js";

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

const SECRET_PATTERNS: readonly RegExp[] = [
  /[A-Za-z0-9_-]*api[_-]?key[A-Za-z0-9_-]*\s*[:=]\s*[^\s,;]+/gi,
  /[A-Za-z0-9_-]*token[A-Za-z0-9_-]*\s*[:=]\s*[^\s,;]+/gi,
  /[A-Za-z0-9_-]*password[A-Za-z0-9_-]*\s*[:=]\s*[^\s,;]+/gi,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g,
];

export function redactStorageDiagnostic(value: unknown): string {
  let text = value instanceof Error ? value.message : String(value);
  for (const pattern of SECRET_PATTERNS) {
    text = text.replace(pattern, "[REDACTED]");
  }
  return text;
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
    cause: input.cause,
  });
}
