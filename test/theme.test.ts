import { test } from "node:test";
import assert from "node:assert/strict";
import { theme } from "../src/tui/theme.js";

test("Theme has all required surface tokens", () => {
  assert.equal(theme.surface.base, "#20251F");
  assert.equal(theme.surface.raised, "#2B332A");
});

test("Theme has all required action tokens", () => {
  assert.equal(theme.action.primary, "#8FB6A0");
  assert.equal(theme.action.attention, "#D8C28B");
});

test("Theme has all required text tokens", () => {
  assert.equal(theme.text.default, "#D8D5C8");
  assert.equal(theme.text.strong, "#E5E1D4");
  assert.equal(theme.text.muted, "#8B8B8B");
});

test("Theme has status error token", () => {
  assert.ok(theme.status.error);
  assert.ok(theme.status.error.startsWith("#"));
});
