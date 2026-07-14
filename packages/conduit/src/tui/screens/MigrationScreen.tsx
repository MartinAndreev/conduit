import { useTheme } from "@tui/components/ThemeProvider.js";
import type { MigrationScreenState } from "@tui/types/migration-screen.js";

export function MigrationScreen({ state }: { state: MigrationScreenState }) {
  const theme = useTheme();
  const progress = `${state.completed}/${state.total}`;
  return (
    <box
      width="100%"
      height="100%"
      backgroundColor={theme.surface.base}
      flexDirection="column"
      padding={2}
    >
      <text content="Conduit · Storage migration" fg={theme.text.strong} />
      <text content="" />
      <box backgroundColor={theme.surface.raised} padding={1}>
        <text
          content={state.error ? "Migration stopped" : state.message}
          fg={state.error ? theme.status.error : theme.text.default}
        />
        <text content={`Progress ${progress}`} fg={theme.text.muted} />
      </box>
      {state.error ? (
        <box flexDirection="column">
          <text content={state.error} fg={theme.status.error} />
          <text
            content="Resolve the problem, then restart Conduit. Press Enter to close."
            fg={theme.text.muted}
          />
        </box>
      ) : (
        <text
          content="Conduit will open the project after storage is ready."
          fg={theme.text.muted}
        />
      )}
    </box>
  );
}
