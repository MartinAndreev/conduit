import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { isSubmitKey } from "@tui/helpers/submit-key.js";
import { useTerminalSubmitKey } from "@tui/hooks/useTerminalSubmitKey.js";

interface RunnableRole {
  readonly name: string;
  readonly runner: string;
  readonly description?: string;
}

export function RoleRunSelectionScreen({
  commandBus,
  queryBus,
  feature,
  onStarted,
  onExit,
}: {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  readonly feature: FeatureReadModel;
  readonly onStarted: (runId: string) => void;
  readonly onExit: () => void;
}) {
  const theme = useTheme();
  const submitKey = useTerminalSubmitKey();
  const [roles, setRoles] = useState<readonly RunnableRole[]>([]);
  const [selected, setSelected] = useState<readonly string[]>([]);
  const [cursor, setCursor] = useState(0);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    void queryBus.execute({ type: "listRunRoles" }).then((result) => {
      if (!result.success) return setError(result.error.message);
      const available = result.data as RunnableRole[];
      setRoles(available);
      setSelected([]);
    });
  }, [queryBus]);
  useKeyboard((event: { name: string; ctrl: boolean }) => {
    if (event.name === "escape" || event.name === "q") return onExit();
    if (event.name === "up")
      return setCursor((value) => Math.max(0, value - 1));
    if (event.name === "down")
      return setCursor((value) => Math.min(roles.length - 1, value + 1));
    if (event.name === "space") {
      const role = roles[cursor];
      if (!role) return;
      return setSelected((current) =>
        current.includes(role.name)
          ? current.filter((name) => name !== role.name)
          : [...current, role.name],
      );
    }
    if (isSubmitKey(event)) {
      if (!selected.length) return setError("Select at least one role.");
      void commandBus
        .dispatch({
          type: "startFeatureRun",
          featureId: feature.id,
          roleNames: selected,
        })
        .then((result) => {
          if (result.success)
            onStarted((result.data as { runId: string }).runId);
          else setError(result.error.message);
        });
    }
  });
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text content={`Run · ${feature.title}`} fg={theme.action.primary} />
      <text
        content={`↑/↓ choose · Space toggle · ${submitKey.label} start · Esc return`}
        fg={theme.text.muted}
      />
      <box
        flexDirection="column"
        flexGrow={1}
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        {roles.map((role, index) => (
          <box key={role.name} flexDirection="column" marginBottom={1}>
            <text
              content={`${index === cursor ? "›" : " "} ${selected.includes(role.name) ? "[x]" : "[ ]"} ${role.name} · ${role.runner}`}
              fg={index === cursor ? theme.action.primary : theme.text.default}
            />
            {role.description && (
              <text content={`    ${role.description}`} fg={theme.text.muted} />
            )}
          </box>
        ))}
        {!roles.length && (
          <text content="Loading configured roles…" fg={theme.text.muted} />
        )}
        {error && <text content={error} fg={theme.status.error} />}
      </box>
    </box>
  );
}
