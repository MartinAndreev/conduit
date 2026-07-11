import type { Theme } from "../theme.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";

interface RefinementPreviewProps {
  theme: Theme;
  values: Record<string, string>;
  architectEnabled: boolean;
}

export function RefinementPreview({
  theme,
  values,
  architectEnabled,
}: RefinementPreviewProps) {
  const markdown = [
    "# Story",
    `## Problem\n${values.problem ?? ""}`,
    `## User\n${values.audience ?? ""}`,
    `## Desired outcome\n${values.outcome ?? ""}`,
    values.constraints
      ? `## Constraints and non-goals\n${values.constraints}`
      : "",
    "# QA test cases",
    values.testCases ?? "",
  ]
    .filter(Boolean)
    .join("\n\n");

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <box
        width="100%"
        height={3}
        flexDirection="row"
        justifyContent="center"
        alignItems="center"
      >
        <text content="Markdown Preview" fg={theme.action.primary} />
      </box>

      <box width="100%" height={2} flexDirection="row" justifyContent="center">
        <text
          content="r: return · a: approve · q: quit · t: toggle architect"
          fg={theme.text.muted}
        />
      </box>

      <box
        width="100%"
        flexGrow={1}
        backgroundColor={theme.surface.raised}
        marginTop={1}
      >
        <MarkdownDocument content={markdown} />
      </box>

      <box
        width="100%"
        height={3}
        flexDirection="row"
        justifyContent="center"
        alignItems="center"
        marginTop={1}
      >
        <text
          content={
            architectEnabled ? "[✓] Architect: ON" : "[ ] Architect: OFF"
          }
          fg={architectEnabled ? theme.action.primary : theme.text.muted}
        />
      </box>

      <box
        width="100%"
        height={2}
        flexDirection="row"
        justifyContent="center"
        alignItems="center"
      >
        <text
          content={
            architectEnabled
              ? "a: approve → start architect"
              : "a: approve → return to home"
          }
          fg={theme.text.muted}
        />
      </box>
    </box>
  );
}
