import { useCallback, useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { formatIndeterminateProgress } from "@helpers/formatting/indeterminate-progress.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import {
  updateScreenActions,
  isUpdateAnimating,
  updateScreenKeyAction,
  updateSuccessGuidance,
} from "@tui/helpers/update-screen-presentation.js";

interface UpdateScreenProps {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  readonly onHome: () => void;
  readonly onQuit: () => void;
}

export function UpdateScreen({
  commandBus,
  queryBus,
  onHome,
  onQuit,
}: UpdateScreenProps) {
  const theme = useTheme();
  const [status, setStatus] = useState<UpdateStatusReadModel>();
  const [frame, setFrame] = useState(0);
  const [queryError, setQueryError] = useState<string>();

  const loadStatus = useCallback(async () => {
    const result = await queryBus.execute({ type: "getUpdateStatus" });
    if (result.success) {
      setStatus(result.data as UpdateStatusReadModel);
      setQueryError(undefined);
    } else {
      setQueryError("Update status is temporarily unavailable.");
    }
  }, [queryBus]);

  useEffect(() => {
    void loadStatus();
    if (
      status &&
      status.status !== UpdateStatus.Updating &&
      status.status !== UpdateStatus.Idle
    )
      return;
    const timer = setInterval(() => void loadStatus(), 100);
    return () => clearInterval(timer);
  }, [loadStatus, status]);

  const updating = isUpdateAnimating(status);
  useEffect(() => {
    if (!updating) return;
    const timer = setInterval(() => setFrame((value) => value + 1), 80);
    return () => clearInterval(timer);
  }, [updating]);

  const retry = useCallback(() => {
    if (status?.status !== UpdateStatus.Failed || !status.retryable) return;
    void commandBus.dispatch({
      type: "startUpdate",
      release: status.release,
      installation: status.installation,
    });
    void loadStatus();
  }, [commandBus, loadStatus, status]);

  useKeyboard((event: { name: string }) => {
    const action = updateScreenKeyAction(status, event.name);
    if (action === "home") onHome();
    if (action === "retry") retry();
    if (action === "quit") onQuit();
  });

  const progress =
    status &&
    (status.status === UpdateStatus.Updating ||
      status.status === UpdateStatus.Succeeded ||
      status.status === UpdateStatus.Failed)
      ? status.progress
      : undefined;
  const failed = status?.status === UpdateStatus.Failed;
  const succeeded = status?.status === UpdateStatus.Succeeded;

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={2}
    >
      <text content="Conduit · Update" fg={theme.text.strong} />
      <text content="" />
      {status && "targetVersion" in status ? (
        <box flexDirection="column">
          <text
            content={`Source v${status.currentVersion} → Target v${status.targetVersion}`}
            fg={theme.text.default}
            wrapMode="word"
          />
          <text
            content={`Method: ${status.installation?.label ?? "Manual update"}`}
            fg={theme.text.muted}
            wrapMode="word"
          />
        </box>
      ) : null}
      <text content="" />
      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={failed ? theme.status.error : theme.action.primary}
        padding={1}
      >
        <text
          content={
            failed
              ? "Update failed"
              : succeeded
                ? "Update complete"
                : (progress?.message ?? "Preparing the update.")
          }
          wrapMode="word"
          fg={
            failed
              ? theme.status.error
              : succeeded
                ? theme.action.primary
                : theme.text.default
          }
        />
        {updating ? (
          <text
            content={formatIndeterminateProgress(frame, 24)}
            fg={theme.action.attention}
          />
        ) : null}
        {progress ? (
          <text content={`Stage: ${progress.stage}`} fg={theme.text.muted} />
        ) : null}
        {failed ? (
          <text
            content={status.message}
            fg={theme.status.error}
            wrapMode="word"
          />
        ) : null}
        {succeeded
          ? updateSuccessGuidance(status).map((line) => (
              <text
                key={line}
                content={line}
                fg={theme.text.default}
                wrapMode="word"
              />
            ))
          : null}
        {queryError ? (
          <text content={queryError} fg={theme.status.error} wrapMode="word" />
        ) : null}
      </box>
      <text content="" />
      <text content={updateScreenActions(status)} fg={theme.text.muted} />
    </box>
  );
}
