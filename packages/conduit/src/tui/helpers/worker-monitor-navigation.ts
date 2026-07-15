import type { WorkerMonitorFocus } from "@tui/types/worker-monitor.js";

export function workerMonitorFocusForKey(
  key: string,
  currentFocus: WorkerMonitorFocus,
  hasChangedFiles: boolean,
): WorkerMonitorFocus | undefined {
  if (key === "1") return "roles";
  if (key === "2") return "activity";
  if (key === "3") return hasChangedFiles ? "files" : undefined;
  if (currentFocus === "roles" && (key === "return" || key === "space")) {
    return hasChangedFiles ? "files" : "activity";
  }
  return undefined;
}
