import { useCallback, useEffect, useMemo, useReducer, useRef } from "react";
import { useKeyboard } from "@opentui/react";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { ChangedFile } from "@domains/runs/types/review.js";
import type { Run } from "@domains/runs/types/run.js";
import {
  deriveRolePresentation,
  extractFileDiff,
} from "@tui/helpers/event-presentation.js";
import type {
  WorkerMonitorAction,
  WorkerMonitorState,
  WorkerMonitorViewModel,
} from "@tui/types/worker-monitor.js";
import { usePollingQuery } from "@tui/hooks/usePollingQuery.js";
import { useSelectableList } from "@tui/hooks/useSelectableList.js";

const initialMonitorState: WorkerMonitorState = {
  events: [],
  roles: [],
  run: undefined,
  selectedRoleIndex: 0,
  diff: undefined,
  changedFiles: [],
  selectedFileIndex: 0,
  totalAdditions: 0,
  totalDeletions: 0,
  loading: true,
  error: null,
  expandedEventIndex: null,
  scrollOffset: 0,
  cancelled: false,
  focusMode: "roles",
  transcriptExpanded: false,
  fileDiffExpanded: false,
};

function monitorReducer(
  state: WorkerMonitorState,
  action: WorkerMonitorAction,
): WorkerMonitorState {
  switch (action.type) {
    case "eventsLoaded":
      return {
        ...state,
        events: action.events,
        roles: action.roles,
        loading: false,
        error: null,
      };
    case "runLoaded":
      return { ...state, run: action.run };
    case "diffLoaded":
      return {
        ...state,
        diff: action.diff,
        changedFiles: action.changedFiles,
        totalAdditions: action.totalAdditions,
        totalDeletions: action.totalDeletions,
      };
    case "loadError":
      return { ...state, error: action.message, loading: false };
    case "selectRole":
      return {
        ...state,
        selectedRoleIndex: action.index,
        expandedEventIndex: null,
        scrollOffset: 0,
        selectedFileIndex: 0,
      };
    case "selectFile":
      return {
        ...state,
        selectedFileIndex: action.index,
        fileDiffExpanded: false,
      };
    case "toggleExpand": {
      const next =
        state.expandedEventIndex === action.index ? null : action.index;
      return { ...state, expandedEventIndex: next };
    }
    case "toggleFocus":
      return {
        ...state,
        focusMode:
          state.focusMode === "roles"
            ? "files"
            : state.focusMode === "files"
              ? "activity"
              : "roles",
      };
    case "setFocus":
      return { ...state, focusMode: action.focus };
    case "toggleTranscript":
      return { ...state, transcriptExpanded: !state.transcriptExpanded };
    case "toggleFileDiff":
      return { ...state, fileDiffExpanded: !state.fileDiffExpanded };
    case "scroll": {
      const max = Math.max(0, state.events.length - 8);
      const next =
        action.direction === "up"
          ? Math.max(0, state.scrollOffset - 1)
          : Math.min(state.scrollOffset + 1, max);
      return { ...state, scrollOffset: next };
    }
    case "cancel":
      return { ...state, cancelled: true };
  }
}

export function useWorkerMonitorController(
  queryBus: QueryBus,
  commandBus: CommandBus,
  runId: string,
  projectRoot: string,
  onExit: () => void,
  enabled: boolean,
  cancelOnExit = false,
): WorkerMonitorViewModel {
  const [state, dispatch] = useReducer(monitorReducer, initialMonitorState);
  const cancellationInFlight = useRef(false);
  const selectRole = useCallback(
    (index: number) => dispatch({ type: "selectRole", index }),
    [],
  );
  const selectFile = useCallback(
    (index: number) => dispatch({ type: "selectFile", index }),
    [],
  );

  const loadRun = useCallback(() => {
    void queryBus
      .execute({ type: "getRun", projectRoot, runId })
      .then((result) => {
        if (result.success) {
          const data = result.data as { run: Run | undefined };
          if (data.run) dispatch({ type: "runLoaded", run: data.run });
        }
      })
      .catch(() => {});
  }, [queryBus, projectRoot, runId]);

  const executeEvents = useCallback(async () => {
    const result = await queryBus.execute({ type: "getRunEvents", runId });
    if (!result.success) throw new Error(result.error.message);
    return result.data as { events: RunnerEvent[]; roleIds: string[] };
  }, [queryBus, runId]);
  const eventQuery = usePollingQuery({
    execute: executeEvents,
    createQuery: useCallback(() => undefined, []),
    project: useCallback(
      (result: { events: RunnerEvent[]; roleIds: string[] }) => result,
      [],
    ),
    enabled,
    intervalMs: 1000,
  });

  useEffect(() => {
    loadRun();
  }, [loadRun]);
  useEffect(() => {
    const eventData = eventQuery.data;
    if (!eventData) return;
    const configuredRoleIds = state.run?.roles.map((role) => role.name) ?? [];
    const roleIds = [
      ...new Set([
        ...configuredRoleIds,
        ...eventData.roleIds.filter((roleId) => roleId !== "system"),
      ]),
    ];
    dispatch({
      type: "eventsLoaded",
      events: eventData.events,
      roles: roleIds.map((roleId) =>
        deriveRolePresentation(eventData.events, roleId),
      ),
    });
  }, [eventQuery.data, state.run]);
  useEffect(() => {
    if (eventQuery.error)
      dispatch({ type: "loadError", message: eventQuery.error });
  }, [eventQuery.error]);

  const loadDiff = useCallback(
    (roleId: string) => {
      void queryBus
        .execute({ type: "getRunDiff", projectRoot, runId, roleId })
        .then((result) => {
          if (result.success) {
            const data = result.data as {
              diff: string | undefined;
              changedFiles: readonly ChangedFile[];
              totalAdditions: number;
              totalDeletions: number;
            };
            dispatch({
              type: "diffLoaded",
              diff: data.diff,
              changedFiles: data.changedFiles,
              totalAdditions: data.totalAdditions,
              totalDeletions: data.totalDeletions,
            });
          }
        })
        .catch(() => {});
    },
    [projectRoot, queryBus, runId],
  );

  const selectedRole = state.roles[state.selectedRoleIndex];
  const roles = useSelectableList({
    itemCount: state.roles.length,
    selectedIndex: state.selectedRoleIndex,
    onSelect: selectRole,
    behavior: "cyclic",
  });
  const files = useSelectableList({
    itemCount: state.changedFiles.length,
    selectedIndex: state.selectedFileIndex,
    onSelect: selectFile,
    behavior: "cyclic",
  });

  useEffect(() => {
    if (selectedRole) loadDiff(selectedRole.roleId);
  }, [selectedRole, loadDiff]);

  const selectedRoleEvents = useMemo(
    () =>
      selectedRole
        ? state.events.filter((e) => e.roleId === selectedRole.roleId)
        : [],
    [state.events, selectedRole],
  );

  // Compute selected file diff from the full diff
  const selectedFileDiff = useMemo(() => {
    if (!state.diff || state.changedFiles.length === 0) return undefined;
    const selectedFile = state.changedFiles[state.selectedFileIndex];
    if (!selectedFile) return undefined;
    return extractFileDiff(state.diff, selectedFile.path);
  }, [state.diff, state.changedFiles, state.selectedFileIndex]);

  const onKey = useCallback(
    (event: { name: string; ctrl?: boolean; shift?: boolean }) => {
      if (!enabled) return;
      if (event.name === "q") {
        if (cancelOnExit) {
          if (!state.cancelled && !cancellationInFlight.current) {
            cancellationInFlight.current = true;
            void commandBus
              .dispatch({ type: "cancelRun", runId })
              .finally(() => {
                cancellationInFlight.current = false;
              });
          }
        }
        return onExit();
      }
      if (event.name === "escape") {
        if (cancelOnExit) {
          if (!state.cancelled && !cancellationInFlight.current) {
            cancellationInFlight.current = true;
            void commandBus
              .dispatch({ type: "cancelRun", runId })
              .finally(() => {
                cancellationInFlight.current = false;
              });
          }
          return onExit();
        }
        if (state.focusMode === "roles") return onExit();
        dispatch({ type: "setFocus", focus: "roles" });
        return;
      }
      if (event.ctrl && event.name === "c") {
        if (state.cancelled || cancellationInFlight.current) return;
        cancellationInFlight.current = true;
        void commandBus
          .dispatch({ type: "cancelRun", runId })
          .then((result) => {
            if (result.success) dispatch({ type: "cancel" });
          })
          .finally(() => {
            cancellationInFlight.current = false;
          });
        return;
      }
      if (event.name === "left" || (event.name === "tab" && event.shift))
        return roles.previous();
      if (event.name === "right" || event.name === "tab") return roles.next();
      if (state.focusMode === "roles") {
        if (event.name === "up") return roles.previous();
        if (event.name === "down") return roles.next();
        if (event.name === "return" || event.name === "space")
          return dispatch({ type: "setFocus", focus: "activity" });
      } else if (state.focusMode === "files") {
        if (event.name === "up" || event.name === "left")
          return files.previous();
        if (event.name === "down" || event.name === "right")
          return files.next();
        if (event.name === "return" || event.name === "space")
          return dispatch({ type: "toggleFileDiff" });
      } else {
        if (event.name === "j" || event.name === "down")
          return dispatch({ type: "scroll", direction: "down" });
        if (event.name === "k" || event.name === "up")
          return dispatch({ type: "scroll", direction: "up" });
        if (event.name === "t") return dispatch({ type: "toggleTranscript" });
        if (event.name === "return" || event.name === "space") {
          if (selectedRoleEvents.length) {
            dispatch({
              type: "toggleExpand",
              index: Math.min(
                state.scrollOffset,
                selectedRoleEvents.length - 1,
              ),
            });
          }
        }
      }
    },
    [
      enabled,
      state.focusMode,
      state.cancelled,
      state.scrollOffset,
      onExit,
      selectedRoleEvents.length,
      commandBus,
      runId,
      cancelOnExit,
      roles,
      files,
    ],
  );
  useKeyboard(onKey);

  return useMemo(
    () => ({
      roles: state.roles,
      selectedRoleIndex: state.selectedRoleIndex,
      events: selectedRoleEvents,
      diff: state.diff,
      selectedFileDiff,
      changedFiles: state.changedFiles,
      selectedFileIndex: state.selectedFileIndex,
      totalAdditions: state.totalAdditions,
      totalDeletions: state.totalDeletions,
      loading: state.loading,
      error: state.error,
      expandedEventIndex: state.expandedEventIndex,
      scrollOffset: state.scrollOffset,
      cancelled: state.cancelled,
      focusMode: state.focusMode,
      transcriptExpanded: state.transcriptExpanded,
      fileDiffExpanded: state.fileDiffExpanded,
      selectRole,
      selectFile,
      toggleExpand: (index: number) =>
        dispatch({ type: "toggleExpand", index }),
      toggleFocus: () => dispatch({ type: "toggleFocus" }),
      toggleTranscript: () => dispatch({ type: "toggleTranscript" }),
      cancel: () => {
        if (state.cancelled || cancellationInFlight.current) return;
        cancellationInFlight.current = true;
        void commandBus
          .dispatch({ type: "cancelRun", runId })
          .then((result) => {
            if (result.success) dispatch({ type: "cancel" });
          })
          .finally(() => {
            cancellationInFlight.current = false;
          });
      },
    }),
    [
      state,
      selectedRoleEvents,
      selectedFileDiff,
      commandBus,
      runId,
      selectFile,
      selectRole,
    ],
  );
}
