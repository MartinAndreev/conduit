import type { Theme } from "@tui/theme.js";

export interface RefinementPacketContent {
  readonly spec: string;
  readonly plan: string;
  readonly tasks: string;
  readonly testCases: string;
}

export function RefinementPacketSummary({
  theme,
  content,
}: {
  theme: Theme;
  content: RefinementPacketContent;
}) {
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
        <text
          content={
            content.spec ||
            content.plan ||
            content.tasks ||
            content.testCases ||
            "No approved packet content found."
          }
          fg={theme.text.default}
        />
      </box>
    </box>
  );
}
