import type { Theme } from "../theme.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import type { ArchitectPreferences } from "@domains/refinement/types/architect-preferences.js";
import { formatRefinementBrief } from "@helpers/formatting/refinement-brief.js";

interface RefinementPreviewProps {
  theme: Theme;
  values: Record<string, string>;
  architectEnabled: boolean;
  researchEnabled: boolean;
  architectPreferences: ArchitectPreferences;
}

export function RefinementPreview({
  theme,
  values,
  architectEnabled,
  researchEnabled,
  architectPreferences,
}: RefinementPreviewProps) {
  const markdown = [
    "# Story",
    formatRefinementBrief({
      problem: values.problem ?? "",
      audience: values.audience ?? "",
      outcome: values.outcome ?? "",
      constraints: values.constraints ?? "",
      guidelines: values.guidelines ?? "",
    }),
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

      <box width="100%" height={3} flexDirection="column" alignItems="center">
        <text
          content="a approve · Ctrl+R edit · q quit"
          fg={theme.text.muted}
        />
        <text
          content="t architect · s research · e effort · l detail"
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
        height={6}
        flexDirection="column"
        paddingLeft={1}
        backgroundColor={theme.surface.raised}
        marginTop={1}
      >
        <box flexDirection="row">
          <text content="Research [s]: " fg={theme.text.muted} />
          <text
            content={researchEnabled ? "ON (preflight)" : "OFF"}
            fg={researchEnabled ? theme.action.attention : theme.text.muted}
          />
        </box>
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
              ? researchEnabled
                ? "Approve: research preflight, then architect"
                : "Approve: start architect"
              : "Approve: save refinement"
          }
          fg={theme.text.muted}
        />
      </box>
    </box>
  );
}
