import { useCallback, useEffect, useMemo, useReducer, useRef } from "react";
import { useKeyboard } from "@opentui/react";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import type { ChangedFile } from "@domains/runs/types/review.js";
import type { Run } from "@domains/runs/types/run.js";
import {
  canonicalMonitorRoleId,
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
import { workerMonitorFocusForKey } from "@tui/helpers/worker-monitor-navigation.js";

const initialMonitorState: WorkerMonitorState = {
  events: [],
  roles: [],
  run: undefined,
  resumeEligibility: undefined,
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
  resuming: false,
  resumeError: null,
  focusMode: "roles",
  transcriptExpanded: false,
  fileDiffExpanded: false,
};

export function monitorReducer(
  state: WorkerMonitorState,
  action: WorkerMonitorAction,
): WorkerMonitorState {
  switch (action.type) {
    case "eventsLoaded": {
      const selectedRole =
        action.roles[state.selectedRoleIndex] ?? action.roles[0];
      const previousRole = state.roles[state.selectedRoleIndex];
      const previousEventCount = previousRole
        ? state.events.filter((event) => event.roleId === previousRole.roleId)
            .length
        : 0;
      const previousMaximum = Math.max(0, previousEventCount - 8);
      const followingLatest = state.scrollOffset >= previousMaximum;
      const nextEventCount = selectedRole
        ? action.events.filter((event) => event.roleId === selectedRole.roleId)
            .length
        : 0;
      const nextMaximum = Math.max(0, nextEventCount - 8);
      return {
        ...state,
        events: action.events,
        roles: action.roles,
        scrollOffset: followingLatest
          ? nextMaximum
          : Math.min(state.scrollOffset, nextMaximum),
        loading: false,
        error: null,
      };
    }
    case "runLoaded":
      return {
        ...state,
        run: action.run,
        resumeEligibility:
          action.run.status === "failed" ? state.resumeEligibility : undefined,
      };
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
    case "selectRole": {
      const selectedRole = state.roles[action.index];
      const selectedEventCount = selectedRole
        ? state.events.filter((event) => event.roleId === selectedRole.roleId)
            .length
        : 0;
      return {
        ...state,
        selectedRoleIndex: action.index,
        expandedEventIndex: null,
        scrollOffset: Math.max(0, selectedEventCount - 8),
        selectedFileIndex: 0,
      };
    }
    case "selectFile":
      return {
        ...state,
        selectedFileIndex: action.index,
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
      const selectedRole = state.roles[state.selectedRoleIndex];
      const selectedEventCount = selectedRole
        ? state.events.filter((event) => event.roleId === selectedRole.roleId)
            .length
        : 0;
      const max = Math.max(0, selectedEventCount - 8);
      const next =
        action.direction === "up"
          ? Math.max(0, state.scrollOffset - 1)
          : Math.min(state.scrollOffset + 1, max);
      return { ...state, scrollOffset: next };
    }
    case "cancel":
      return { ...state, cancelled: true };
    case "resumeEligibilityLoaded":
      return { ...state, resumeEligibility: action.eligibility };
    case "resumeStarted":
      return {
        ...state,
        resuming: true,
        resumeError: null,
        resumeEligibility: undefined,
      };
    case "resumeFinished":
      return { ...state, resuming: false, resumeError: null };
    case "resumeFailed":
      return { ...state, resuming: false, resumeError: action.message };
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
  const resumeInFlight = useRef(false);
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

  const loadResumeEligibility = useCallback(() => {
    if (state.run?.status !== "failed") return;
    void queryBus
      .execute({ type: "getRunResumeEligibility", projectRoot, runId })
      .then((result) => {
        if (result.success)
          dispatch({
            type: "resumeEligibilityLoaded",
            eligibility:
              result.data as import("@domains/runs/types/resume-eligibility.js").ResumeEligibility,
          });
      })
      .catch(() => {});
  }, [projectRoot, queryBus, runId, state.run?.status]);

  useEffect(() => {
    loadResumeEligibility();
  }, [loadResumeEligibility]);

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
    if (eventQuery.data) loadRun();
  }, [eventQuery.data, loadRun]);
  useEffect(() => {
    const eventData = eventQuery.data;
    if (!eventData) return;
    const configuredRoleIds = state.run?.roles.map((role) => role.name) ?? [];
    const events = eventData.events.map((event) => {
      const roleId = canonicalMonitorRoleId(event.roleId, configuredRoleIds);
      return roleId === event.roleId ? event : { ...event, roleId };
    });
    const roleIds = [
      ...new Set([
        ...configuredRoleIds,
        ...eventData.roleIds
          .filter((roleId) => roleId !== "system")
          .map((roleId) => canonicalMonitorRoleId(roleId, configuredRoleIds)),
      ]),
    ];
    dispatch({
      type: "eventsLoaded",
      events,
      roles: roleIds.map((roleId) => deriveRolePresentation(events, roleId)),
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
      if (
        !event.ctrl &&
        event.name === "r" &&
        state.resumeEligibility?.state === "resumable" &&
        !resumeInFlight.current
      ) {
        resumeInFlight.current = true;
        dispatch({ type: "resumeStarted" });
        void commandBus
          .dispatch({ type: "resumeRun", runId })
          .then((result) => {
            if (!result.success) {
              dispatch({ type: "resumeFailed", message: result.error.message });
              loadResumeEligibility();
              return;
            }
            dispatch({ type: "resumeFinished" });
            loadRun();
            loadResumeEligibility();
          })
          .catch((cause) => {
            dispatch({
              type: "resumeFailed",
              message: cause instanceof Error ? cause.message : String(cause),
            });
            loadResumeEligibility();
          })
          .finally(() => {
            resumeInFlight.current = false;
          });
        return;
      }
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
      const requestedFocus = workerMonitorFocusForKey(
        event.name,
        state.focusMode,
        state.changedFiles.length > 0,
      );
      if (requestedFocus) {
        return dispatch({ type: "setFocus", focus: requestedFocus });
      }
      if (event.name === "left" || (event.name === "tab" && event.shift))
        return roles.previous();
      if (event.name === "right" || event.name === "tab") return roles.next();
      if (state.focusMode === "roles") {
        if (event.name === "up") return roles.previous();
        if (event.name === "down") return roles.next();
      } else if (state.focusMode === "files") {
        if (
          !state.fileDiffExpanded &&
          (event.name === "up" || event.name === "left")
        )
          return files.previous();
        if (
          !state.fileDiffExpanded &&
          (event.name === "down" || event.name === "right")
        )
          return files.next();
        if (state.fileDiffExpanded && event.name === "left")
          return files.previous();
        if (state.fileDiffExpanded && event.name === "right")
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
      state.fileDiffExpanded,
      state.cancelled,
      state.run?.status,
      state.resumeEligibility?.state,
      state.scrollOffset,
      state.changedFiles.length,
      onExit,
      selectedRoleEvents.length,
      commandBus,
      runId,
      cancelOnExit,
      roles,
      files,
      loadRun,
      loadResumeEligibility,
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
      canResume: state.resumeEligibility?.state === "resumable",
      showRecovery: state.run?.status === "failed",
      resumeEligibility: state.resumeEligibility,
      resuming: state.resuming,
      resumeError: state.resumeError,
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
