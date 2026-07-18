const SECRET_PATTERNS: readonly RegExp[] = [
  /["']?(?:api[_-]?key|token|password|secret)["']?\s*[:=]\s*["']?[^\s,;}"']+/gi,
  /\bBearer\s+[A-Za-z0-9._~+/=-]+/gi,
  /\b(?:sk|ghp|github_pat)_[A-Za-z0-9_-]{12,}\b/g,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g,
];

const SECRET_KEY = /(?:api[_-]?key|token|password|secret|private[_-]?key)/i;
const NON_SECRET_TOKEN_KEY = /^workspaceFencingToken$/;

function isSecretKey(key: string): boolean {
  return SECRET_KEY.test(key) && !NON_SECRET_TOKEN_KEY.test(key);
}

function escapePattern(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function environmentSecrets(): readonly string[] {
  return Object.entries(process.env)
    .filter(([key, value]) => isSecretKey(key) && (value?.length ?? 0) >= 8)
    .map(([, value]) => value!);
}

export function redactSecrets(value: string): string {
  let redacted = value;
  for (const pattern of SECRET_PATTERNS)
    redacted = redacted.replace(pattern, "[REDACTED]");
  for (const secret of environmentSecrets())
    redacted = redacted.replace(
      new RegExp(escapePattern(secret), "g"),
      "[REDACTED]",
    );
  return redacted;
}

export function containsSecret(value: string): boolean {
  return redactSecrets(value) !== value;
}

export function redactPersistedValue<T>(value: T): T {
  if (typeof value === "string") return redactSecrets(value) as T;
  if (Array.isArray(value))
    return value.map((item) => redactPersistedValue(item)) as T;
  if (value && typeof value === "object") {
    const redacted = Object.fromEntries(
      Object.entries(value).map(([key, item]) =>
        isSecretKey(key)
          ? [key, "[REDACTED]"]
          : [key, redactPersistedValue(item)],
      ),
    );
    return redacted as T;
  }
  return value;
}
