import type { KeyEvent } from "@opentui/core";

export function isSubmitKey(
  event: Pick<KeyEvent, "ctrl" | "meta" | "name">,
): boolean {
  return (
    event.name === "f10" ||
    (event.name === "return" && (event.ctrl || event.meta))
  );
}

export function getSubmitKeyLabel(kittyKeyboardSupported: boolean): string {
  return kittyKeyboardSupported ? "Ctrl+Enter" : "Alt+Enter";
}
