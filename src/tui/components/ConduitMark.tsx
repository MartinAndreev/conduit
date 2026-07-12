import type { Theme } from "@tui/theme.js";

interface ConduitMarkProps {
  theme: Theme;
}

/** A compact brand mark that remains legible in the sidebar. */
export function ConduitMark({ theme }: ConduitMarkProps) {
  return (
    <box
      flexDirection="column"
      paddingLeft={1}
      paddingTop={1}
      paddingBottom={1}
    >
      <text content="      ╭────●" fg={theme.action.primary} />
      <box flexDirection="row">
        <text content=" ●────┤    ●  " fg={theme.text.default} />
        <text content="conduit" fg={theme.text.strong} />
      </box>
      <text content="      ╰────●" fg={theme.action.attention} />
    </box>
  );
}
