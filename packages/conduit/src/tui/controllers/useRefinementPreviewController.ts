import { useCallback } from "react";
import { useKeyboard } from "@opentui/react";

export function useRefinementPreviewController(
  actions: {
    readonly approve: () => void;
    readonly reject: () => void;
    readonly quit: () => void;
    readonly toggleArchitect: () => void;
    readonly toggleResearch: () => void;
    readonly cycleArchitectPreference: (kind: "effort" | "detailLevel") => void;
  },
  enabled: boolean,
): void {
  const onKey = useCallback(
    (event: { name: string; ctrl: boolean }) => {
      if (!enabled) return;
      if (event.name === "a") actions.approve();
      if (event.ctrl && event.name === "r") actions.reject();
      if (event.name === "q") actions.quit();
      if (event.name === "t") actions.toggleArchitect();
      if (event.name === "s") actions.toggleResearch();
      if (event.name === "e") actions.cycleArchitectPreference("effort");
      if (event.name === "l") actions.cycleArchitectPreference("detailLevel");
    },
    [actions, enabled],
  );
  useKeyboard(onKey);
}
