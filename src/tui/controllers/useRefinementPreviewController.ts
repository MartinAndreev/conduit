import { useCallback } from "react";
import { useKeyboard } from "@opentui/react";

export function useRefinementPreviewController(
  actions: {
    readonly approve: () => void;
    readonly reject: () => void;
    readonly quit: () => void;
    readonly toggleArchitect: () => void;
  },
  enabled: boolean,
): void {
  const onKey = useCallback(
    (event: { name: string }) => {
      if (!enabled) return;
      if (event.name === "a") actions.approve();
      if (event.name === "r") actions.reject();
      if (event.name === "q") actions.quit();
      if (event.name === "t") actions.toggleArchitect();
    },
    [actions, enabled],
  );
  useKeyboard(onKey);
}
