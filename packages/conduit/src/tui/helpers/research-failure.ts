import type { RunnerEvent } from "@domains/runs/types/runner-events.js";

const TRACE_LIMIT = 1_200;

function remediationFor(message: string): string | undefined {
  if (
    /could not create worktree|git worktree|post-checkout|husky/i.test(message)
  )
    return "Git could not prepare the isolated worktree because a repository checkout hook failed. Install the project dependencies and verify its post-checkout/Husky hook, then retry.";
  if (/\bENOENT\b|not found|not recognized|cannot find/i.test(message))
    return "The configured researcher runner was not found. Check the researcher runner setting and ensure its executable is on PATH.";
  if (/permission denied|\bEACCES\b|\bEPERM\b/i.test(message))
    return "The researcher runner could not be executed. Check executable permissions and sandbox policy.";
  return undefined;
}

export function formatResearchFailure(
  events: readonly RunnerEvent[],
  runId: string,
): string {
  const researcherEvents = events.filter(
    (event) => event.roleId === "researcher",
  );
  const errorEvent = [...researcherEvents]
    .reverse()
    .find((event) => event.type === "error" && event.payload.kind === "error");
  const resultEvent = [...researcherEvents]
    .reverse()
    .find(
      (event) => event.type === "result" && event.payload.kind === "result",
    );
  const stderrEvent = [...researcherEvents]
    .reverse()
    .find(
      (event) =>
        event.type === "tool-output" &&
        event.payload.kind === "tool-output" &&
        event.payload.tool === "runner stderr" &&
        event.payload.output.trim(),
    );

  const error =
    errorEvent?.payload.kind === "error" ? errorEvent.payload : undefined;
  const result =
    resultEvent?.payload.kind === "result" ? resultEvent.payload : undefined;
  const stderr =
    stderrEvent?.payload.kind === "tool-output"
      ? stderrEvent.payload.output.trim().slice(0, TRACE_LIMIT)
      : "";
  const headline = error
    ? `Researcher failed (${error.code}): ${error.message}`
    : `Researcher failed${result ? ` with exit code ${result.exitCode}` : ""}.`;
  const remediation = remediationFor(`${error?.message ?? ""}\n${stderr}`);
  const details = [
    headline,
    stderr && stderr !== error?.message ? `Runner stderr:\n${stderr}` : "",
    remediation ?? "Review the runner error and configuration, then retry.",
    `Run ID: ${runId}`,
  ].filter(Boolean);
  return details.join("\n\n");
}
