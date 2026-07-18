import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { WorkspaceContinuity } from "@domains/runs/types/workspace-continuity.js";
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
  const [continuity, setContinuity] = useState<WorkspaceContinuity>();
  const [checkingContinuity, setCheckingContinuity] = useState(false);
  const [confirmingStartNew, setConfirmingStartNew] = useState(false);
  const [starting, setStarting] = useState(false);
  useEffect(() => {
    void queryBus.execute({ type: "listRunRoles" }).then((result) => {
      if (!result.success) return setError(result.error.message);
      const available = result.data as RunnableRole[];
      setRoles(available);
      setSelected([]);
    });
  }, [queryBus]);
  useEffect(() => {
    setConfirmingStartNew(false);
    if (!selected.length) {
      setContinuity(undefined);
      return;
    }
    let active = true;
    setCheckingContinuity(true);
    void queryBus
      .execute({
        type: "getWorkspaceContinuity",
        featureId: feature.id,
        roleNames: selected,
      })
      .then((result) => {
        if (!active) return;
        if (result.success) setContinuity(result.data as WorkspaceContinuity);
        else setError(result.error.message);
      })
      .finally(() => {
        if (active) setCheckingContinuity(false);
      });
    return () => {
      active = false;
    };
  }, [feature.id, queryBus, selected]);

  const start = (
    mode: "continue" | "start-new",
    confirmDiscardRetained = false,
  ) => {
    if (starting) return;
    setStarting(true);
    setError(null);
    const dispatched = commandBus.dispatch({
      type: "startFeatureRun",
      featureId: feature.id,
      roleNames: selected,
      mode,
      confirmDiscardRetained,
    });
    if (mode === "continue" && continuity?.state === "compatible-continue") {
      onStarted(continuity.runId);
      void dispatched;
      return;
    }
    void dispatched
      .then((result) => {
        if (result.success) onStarted((result.data as { runId: string }).runId);
        else setError(result.error.message);
      })
      .finally(() => setStarting(false));
  };

  useKeyboard((event: { name: string; ctrl: boolean; meta: boolean }) => {
    if (confirmingStartNew) {
      if (event.name === "y") start("start-new", true);
      if (event.name === "n" || event.name === "escape" || isSubmitKey(event))
        setConfirmingStartNew(false);
      return;
    }
    if (event.name === "escape" || event.name === "q") return onExit();
    if (continuity?.state === "lease-conflict") return;
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
    if (event.name === "c" && continuity?.state === "compatible-continue")
      return start("continue");
    if (event.name === "n") {
      if (continuity?.state === "no-retained") return start("start-new");
      if (continuity) return setConfirmingStartNew(true);
    }
    if (isSubmitKey(event)) {
      if (!selected.length) return setError("Select at least one role.");
      if (checkingContinuity || !continuity)
        return setError("Wait for workspace continuity checking to finish.");
      if (continuity.state === "compatible-continue") return start("continue");
      if (continuity.state === "no-retained") return start("start-new");
      return setError("Choose Start Anew explicitly to discard retained work.");
    }
  });

  const continuityText = checkingContinuity
    ? "Checking retained role work…"
    : continuity?.state === "compatible-continue"
      ? `Continue run ${continuity.runId} · preserve ${continuity.preservedRoles.join(", ") || "none"} · retry ${continuity.retryRoles.join(", ")}`
      : continuity?.state === "incompatible-retained"
        ? `Retained run ${continuity.runId ?? "multiple"} is incompatible: ${continuity.reason}`
        : continuity?.state === "lease-conflict"
          ? continuity.reason
          : continuity?.state === "no-retained"
            ? "No retained work. A new run will start."
            : undefined;
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
        content={
          continuity?.state === "lease-conflict"
            ? "Retained workspace is leased · Esc Cancel"
            : `↑/↓ choose · Space toggle · ${submitKey.label} default · [c] continue · [n] start anew · Esc return`
        }
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
        {continuityText && (
          <text content={continuityText} fg={theme.action.attention} />
        )}
        {confirmingStartNew && continuity && (
          <text
            content={`Start Anew will abandon retained run${continuity.state === "incompatible-retained" && continuity.runIds.length > 1 ? "s" : ""} ${continuity.state === "incompatible-retained" ? continuity.runIds.join(", ") : "runId" in continuity ? continuity.runId : "unknown"} for roles: ${continuity.roles.join(", ")}. [y] Confirm · [n/Esc/${submitKey.label}] Cancel (default)`}
            fg={theme.status.error}
          />
        )}
        {starting && <text content="Starting run…" fg={theme.text.muted} />}
        {error && <text content={error} fg={theme.status.error} />}
      </box>
    </box>
  );
}
