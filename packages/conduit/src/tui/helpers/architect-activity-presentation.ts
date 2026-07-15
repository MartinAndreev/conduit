import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";

export const architectActivityCopy = {
  heading: "Activity",
  loading: "Loading architect activity...",
  changedFilesHeading: "Changed files",
  emptyActivity:
    "No structured activity yet. The architect may be reading project context.",
  keyboardHelp:
    "Keys: ↑/↓ select a changed file · Enter open/close its diff · Esc close an open diff · Esc again or q cancels refinement and exits",
} as const;

export function architectActivitySummary(
  changedFileCount: number,
  eventCount: number,
): string {
  return `Changed files: ${changedFileCount} | Events: ${eventCount}`;
}

export function architectCurrentActivity(
  featureId: string,
  latestEvent?: ArchitectEvent,
): string {
  if (!latestEvent) return `Refining feature ${featureId}`;
  switch (latestEvent.type) {
    case "tool-call":
      return "Running a command";
    case "tool-output":
      return "Command completed";
    case "file-change":
    case "patch":
      return "Updating the feature packet";
    case "error":
      return "Architect reported an error";
    case "thought":
    case "activity":
    case "lifecycle": {
      const content = latestEvent.content.replace(/\s+/g, " ").trim();
      return content.length <= 120 ? content : `${content.slice(0, 119)}…`;
    }
  }
}

export function architectRunningStatus(latestOutputTime?: string): string {
  if (latestOutputTime) {
    return `Process is still running · last structured output ${latestOutputTime}`;
  }
  return "Process is still running · waiting for the first structured runner event";
}
