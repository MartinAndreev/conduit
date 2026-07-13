import { describe, expect, test } from "bun:test";
import { parseKeypress } from "@opentui/core";
import { getSubmitKeyLabel, isSubmitKey } from "@tui/helpers/submit-key.js";

describe("terminal submit key", () => {
  test("does not mistake legacy Enter for Ctrl+Enter", () => {
    const event = parseKeypress("\r", { useKittyKeyboard: true });

    expect(event).not.toBeNull();
    expect(event && isSubmitKey(event)).toBe(false);
  });

  test("accepts Kitty Ctrl+Enter", () => {
    const event = parseKeypress("\u001b[13;5u", {
      useKittyKeyboard: true,
    });

    expect(event).not.toBeNull();
    expect(event && isSubmitKey(event)).toBe(true);
  });

  test("accepts F10 as the legacy-terminal fallback", () => {
    const event = parseKeypress("\u001b[21~", { useKittyKeyboard: true });

    expect(event).not.toBeNull();
    expect(event && isSubmitKey(event)).toBe(true);
  });

  test("advertises Ctrl+Enter only after capability detection", () => {
    expect(getSubmitKeyLabel(false)).toBe("F10");
    expect(getSubmitKeyLabel(true)).toBe("Ctrl+Enter");
  });
});
