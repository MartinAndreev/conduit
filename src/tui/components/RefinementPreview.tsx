import type { Theme } from "../theme.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import type { ArchitectPreferences } from "@domains/refinement/types/architect-preferences.js";

interface RefinementPreviewProps {
  theme: Theme;
  values: Record<string, string>;
  architectEnabled: boolean;
  architectPreferences: ArchitectPreferences;
}

export function RefinementPreview({
  theme,
  values,
  architectEnabled,
  architectPreferences,
}: RefinementPreviewProps) {
  const markdown = [
    "# Story",
    `## Problem\n${values.problem ?? ""}`,
    `## User\n${values.audience ?? ""}`,
    `## Desired outcome\n${values.outcome ?? ""}`,
    values.constraints
      ? `## Constraints and non-goals\n${values.constraints}`
      : "",
    values.guidelines
      ? `## Implementation and design guidance\n${values.guidelines}`
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
          content="r: return · a: approve/start architect · q: quit · t: toggle architect · e: effort · l: detail"
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
        height={5}
        flexDirection="column"
        paddingLeft={1}
        backgroundColor={theme.surface.raised}
        marginTop={1}
      >
        <box flexDirection="row">
          <text content="Architect: " fg={theme.text.muted} />
          <text
            content={architectEnabled ? "ON (t toggle)" : "OFF (t toggle)"}
            fg={architectEnabled ? theme.action.primary : theme.text.muted}
          />
        </box>
        <box flexDirection="row">
          <text content="Effort [e]: " fg={theme.text.muted} />
          <text
            content={architectPreferences.effort}
            fg={theme.action.attention}
          />
        </box>
        <box flexDirection="row">
          <text content="Detail [l]: " fg={theme.text.muted} />
          <text
            content={architectPreferences.detailLevel}
            fg={theme.action.primary}
          />
        </box>
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
              ? "a: approve → start architect · e: cycle effort · l: cycle detail"
              : "a: approve → return to home"
          }
          fg={theme.text.muted}
        />
      </box>
    </box>
  );
}
