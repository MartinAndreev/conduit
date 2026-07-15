import type { Theme } from "@tui/theme.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";
import type { RefinementPacketContent } from "@tui/types/refinement.js";

export function RefinementPacketSummary({
  theme,
  content,
}: {
  theme: Theme;
  content: RefinementPacketContent;
}) {
  const markdown = [
    content.story,
    content.spec,
    content.plan,
    content.tasks,
    content.testCases,
  ]
    .map((section) => section.trim())
    .filter(Boolean)
    .join("\n\n---\n\n");

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <box
        flexDirection="column"
        height={5}
        paddingLeft={1}
        paddingRight={1}
        paddingTop={1}
        paddingBottom={1}
        backgroundColor={theme.surface.raised}
      >
        <text
          content="Existing refined packet"
          fg={theme.action.primary}
          height={1}
        />
        <text
          content="The packet files are now the source of truth."
          fg={theme.text.default}
          height={1}
        />
        <text
          content="e  Edit original brief    q  Return home"
          fg={theme.text.muted}
          height={1}
        />
      </box>
      <box
        flexGrow={1}
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        <MarkdownDocument
          content={markdown || "No approved packet content found."}
        />
      </box>
    </box>
  );
}
