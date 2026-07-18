import { test } from "bun:test";
import assert from "node:assert/strict";
import { redactPersistedValue } from "../src/system/storage/security/secret-redaction.js";

test("persisted redaction preserves non-secret workspace fencing tokens", () => {
  const value = redactPersistedValue({
    workspaceFencingToken: 7,
    apiToken: "sensitive-value",
  });
  assert.equal(value.workspaceFencingToken, 7);
  assert.equal(value.apiToken, "[REDACTED]");
});
