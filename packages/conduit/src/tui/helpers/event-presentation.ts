import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { RolePresentation } from "@tui/types/worker-monitor.js";

const readOperationPattern =
  /(?:^|\s)(?:cat|find|grep|head|less|ls|read|rg|sed|tail)(?:\s|$)/i;
const writeOperationPattern =
  /(?:apply_patch|(?:^|\s)(?:cp|mkdir|mv|rm|touch|write)(?:\s|$))/i;

export function canonicalMonitorRoleId(
  roleId: string,
  configuredRoleIds: readonly string[],
): string {
  if (configuredRoleIds.includes(roleId)) return roleId;
  return (
    configuredRoleIds.find(
      (configured) =>
        roleId.startsWith(`${configured}-resume-`) ||
        roleId.startsWith(`${configured}-auto-retry-`),
    ) ?? roleId
  );
}

function operationPhase(operation: string): string {
  if (writeOperationPattern.test(operation)) {
    return "writing";
  }
  if (readOperationPattern.test(operation)) {
    return "reading";
  }
  return "running";
}

export function activityPhaseForEvent(event: RunnerEvent): string {
  const { payload } = event;
  switch (payload.kind) {
    case "lifecycle":
      if (payload.state === "starting") {
        return "starting";
      }
      return payload.state;
    case "activity":
      return "thinking";
    case "tool-call":
      return operationPhase(`${payload.tool} ${payload.args ?? ""}`);
    case "tool-output":
      return operationPhase(payload.tool);
    case "file-change":
    case "patch":
      return "writing";
    case "error":
      return "failed";
    case "result":
      return payload.exitCode === 0 ? "completed" : "failed";
  }
}

function bounded(value: string, maximum = 160): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (normalized.length <= maximum) {
    return normalized;
  }
  return `${normalized.slice(0, maximum - 1)}…`;
}

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
    message = activityPhaseForEvent(lastLifecycle);
  }

  const terminal = state === "completed" || state === "failed";
  if (!isUnavailable && !terminal) {
    const latestEvent = roleEvents.at(-1);
    if (latestEvent) {
      state = "working";
      message = activityPhaseForEvent(latestEvent);
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
      return bounded(payload.message);
    case "tool-call":
      return `Called ${payload.tool}${payload.args ? `(${bounded(payload.args, 100)})` : ""}`;
    case "tool-output":
      return `${payload.tool} → ${bounded(payload.output, 120)}${payload.truncated ? "…" : ""}`;
    case "file-change":
      return `Changed ${payload.path} (+${payload.additions} -${payload.deletions})`;
    case "patch":
      return `Patch: ${payload.fileCount} files`;
    case "error":
      return `[${payload.code}] ${bounded(payload.message)}`;
    case "result":
      return `Exit ${payload.exitCode}: ${bounded(payload.summary)}`;
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
