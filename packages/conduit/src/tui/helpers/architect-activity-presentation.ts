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
  latestActivity?: string,
): string {
  return latestActivity ?? `Refining feature ${featureId}`;
}

export function architectRunningStatus(latestOutputTime?: string): string {
  if (latestOutputTime) {
    return `Process is still running · last structured output ${latestOutputTime}`;
  }
  return "Process is still running · waiting for the first structured runner event";
}
