import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { RolePresentation } from "@tui/types/worker-monitor.js";

export function deriveRolePresentation(
  events: readonly RunnerEvent[],
  roleId: string,
): RolePresentation {
  const roleEvents = events.filter((e) => e.roleId === roleId);
  const lifecycleEvents = roleEvents.filter((e) => e.type === "lifecycle");
  const lastLifecycle = lifecycleEvents[lifecycleEvents.length - 1];

  let state: RolePresentation["state"] = "waiting";
  let message = "queued";
  let isUnavailable = false;

  if (lastLifecycle?.payload.kind === "lifecycle") {
    const lifecycleState = lastLifecycle.payload.state;
    if (lifecycleState === "completed") state = "completed";
    else if (lifecycleState === "failed" || lifecycleState === "cancelled")
      state = "failed";
    else if (lifecycleState === "unavailable") {
      state = "failed";
      isUnavailable = true;
    } else state = "working";
    message = lastLifecycle.payload.message ?? lifecycleState;
  }

  if (!isUnavailable) {
    const lastActivity = [...roleEvents]
      .reverse()
      .find((e) => e.type === "activity");
    if (lastActivity?.payload.kind === "activity") {
      message = lastActivity.payload.message;
    }
  }

  return {
    roleId,
    state,
    message,
    eventCount: roleEvents.length,
    isUnavailable,
  };
}

export function formatEventDescription(event: RunnerEvent): string {
  const { payload } = event;
  switch (payload.kind) {
    case "lifecycle":
      return payload.message ?? `State: ${payload.state}`;
    case "activity":
      return payload.message;
    case "tool-call":
      return `Called ${payload.tool}${payload.args ? `(${payload.args.slice(0, 60)})` : ""}`;
    case "tool-output":
      return `${payload.tool} → ${payload.output.slice(0, 80)}${payload.truncated ? "…" : ""}`;
    case "file-change":
      return `Changed ${payload.path} (+${payload.additions} -${payload.deletions})`;
    case "patch":
      return `Patch: ${payload.fileCount} files`;
    case "error":
      return `[${payload.code}] ${payload.message}`;
    case "result":
      return `Exit ${payload.exitCode}: ${payload.summary}`;
  }
}

export function extractFileDiff(
  patch: string,
  file: string,
): string | undefined {
  const sections = patch.split(/(?=^diff --git a\/)/m);
  return sections.find(
    (section) =>
      section.startsWith(`diff --git a/${file} b/${file}`) ||
      section.startsWith(`diff --git a/dev/null b/${file}`),
  );
}
