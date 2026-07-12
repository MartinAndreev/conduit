import type { Theme } from "../theme.js";
import type { ReviewFinding } from "@domains/runs/types/review.js";

interface ReviewResultPanelProps {
  readonly reviewId: string | undefined;
  readonly decision: "approved" | "rejected" | undefined;
  readonly findings: readonly ReviewFinding[];
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
  readonly reviewedAt: string | undefined;
  readonly theme: Theme;
}

function severityColor(
  severity: "info" | "warning" | "error",
  theme: Theme,
): string {
  switch (severity) {
    case "error":
      return theme.status.error;
    case "warning":
      return theme.action.attention;
    default:
      return theme.text.muted;
  }
}

export function ReviewResultPanel({
  reviewId,
  decision,
  findings,
  evidencePaths,
  followUp,
  reviewedAt,
  theme,
}: ReviewResultPanelProps) {
  if (!reviewId) {
    return (
      <box flexDirection="column" padding={1}>
        <text content="No review submitted yet" fg={theme.text.muted} />
      </box>
    );
  }

  const statusIcon = decision === "approved" ? "✓" : "×";
  const statusColor =
    decision === "approved" ? theme.action.primary : theme.status.error;

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row">
        <text content={`${statusIcon} `} fg={statusColor} />
        <text content={`Review: ${decision?.toUpperCase()}`} fg={statusColor} />
        {reviewedAt && (
          <text content={`  ${reviewedAt}`} fg={theme.text.muted} />
        )}
      </box>
      <text content="" />

      {findings.length > 0 && (
        <box flexDirection="column">
          <text
            content={`Findings (${findings.length}):`}
            fg={theme.text.strong}
          />
          {findings.map((finding, index) => (
            <box key={index} flexDirection="row">
              <text
                content={`  [${finding.severity.toUpperCase()}] `}
                fg={severityColor(finding.severity, theme)}
              />
              {finding.file && (
                <text
                  content={`${finding.file}${finding.line ? `:${finding.line}` : ""} `}
                  fg={theme.action.primary}
                />
              )}
              <text content={finding.message} fg={theme.text.default} />
            </box>
          ))}
        </box>
      )}

      {evidencePaths.length > 0 && (
        <box flexDirection="column" marginTop={1}>
          <text content="Evidence:" fg={theme.text.strong} />
          {evidencePaths.map((path) => (
            <text key={path} content={`  ${path}`} fg={theme.text.muted} />
          ))}
        </box>
      )}

      {followUp && (
        <box flexDirection="column" marginTop={1}>
          <text content="Follow-up:" fg={theme.text.strong} />
          <text content={`  ${followUp}`} fg={theme.text.default} />
        </box>
      )}
    </box>
  );
}
