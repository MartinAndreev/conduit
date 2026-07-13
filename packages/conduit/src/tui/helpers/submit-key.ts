import type { KeyEvent } from "@opentui/core";

export function isSubmitKey(event: Pick<KeyEvent, "ctrl" | "name">): boolean {
  return event.name === "f10" || (event.name === "return" && event.ctrl);
}

export function getSubmitKeyLabel(kittyKeyboardSupported: boolean): string {
  return kittyKeyboardSupported ? "Ctrl+Enter" : "F10";
}
