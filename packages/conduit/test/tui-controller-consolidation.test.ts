import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { test } from "bun:test";
import {
  moveSelectedIndex,
  normalizeSelectedIndex,
} from "../src/tui/hooks/useSelectableList.js";
import { extractFileDiff } from "../src/tui/helpers/event-presentation.js";
import { workerMonitorFocusForKey } from "../src/tui/helpers/worker-monitor-navigation.js";
import { monitorReducer } from "../src/tui/controllers/useWorkerMonitorController.js";
import type { WorkerMonitorState } from "../src/tui/types/worker-monitor.js";

test("selectable list bounds selection and safely resets an empty list", () => {
  assert.equal(normalizeSelectedIndex(7, 3), 2);
  assert.equal(normalizeSelectedIndex(-2, 3), 0);
  assert.equal(normalizeSelectedIndex(2, 0), 0);
  assert.equal(moveSelectedIndex(2, 3, "next", "bounded"), 2);
  assert.equal(moveSelectedIndex(0, 3, "previous", "bounded"), 0);
});

test("selectable list cycles when requested", () => {
  assert.equal(moveSelectedIndex(2, 3, "next", "cyclic"), 0);
  assert.equal(moveSelectedIndex(0, 3, "previous", "cyclic"), 2);
});

test("a changed-file selection extracts only that unified file diff", () => {
  const diff = [
    "diff --git a/a.ts b/a.ts",
    "--- a/a.ts",
    "+++ b/a.ts",
    "@@ -1 +1 @@",
    "-old",
    "+new",
    "diff --git a/b.ts b/b.ts",
    "--- a/b.ts",
    "+++ b/b.ts",
    "@@ -1 +1 @@",
    "-before",
    "+after",
  ].join("\n");
  const selected = extractFileDiff(diff, "b.ts");
  assert.ok(selected?.includes("b.ts"));
  assert.ok(!selected?.includes("a.ts"));
});

test("worker monitor keyboard navigation can reach changed files", () => {
  assert.equal(workerMonitorFocusForKey("3", "roles", true), "files");
  assert.equal(workerMonitorFocusForKey("return", "roles", true), "files");
  assert.equal(workerMonitorFocusForKey("space", "roles", false), "activity");
  assert.equal(workerMonitorFocusForKey("3", "roles", false), undefined);
});

test("selecting the next file keeps an open diff preview expanded", () => {
  const state: WorkerMonitorState = {
    events: [],
    roles: [],
    run: undefined,
    resumeEligibility: undefined,
    selectedRoleIndex: 0,
    diff: "diff",
    changedFiles: [
      { path: "a.ts", additions: 1, deletions: 0 },
      { path: "b.ts", additions: 1, deletions: 0 },
    ],
    selectedFileIndex: 0,
    totalAdditions: 2,
    totalDeletions: 0,
    loading: false,
    error: null,
    expandedEventIndex: null,
    scrollOffset: 0,
    cancelled: false,
    resuming: false,
    resumeError: null,
    focusMode: "files",
    transcriptExpanded: false,
    fileDiffExpanded: true,
  };

  const next = monitorReducer(state, { type: "selectFile", index: 1 });

  assert.equal(next.selectedFileIndex, 1);
  assert.equal(next.fileDiffExpanded, true);
});

test("worker monitor reserves lowercase r for failed-run resume", () => {
  const state: WorkerMonitorState = {
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
    loading: false,
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
  assert.equal(monitorReducer(state, { type: "resumeStarted" }).resuming, true);
  assert.equal(
    monitorReducer(state, { type: "resumeFailed", message: "failed" })
      .resumeError,
    "failed",
  );
});

test("controllers do not own renderer primitives or exported view contracts", async () => {
  const root = process.cwd();
  const controllers = await Promise.all(
    [
      "useArchitectActivityController.ts",
      "useHomeController.ts",
      "useRefinementController.ts",
      "useWorkerMonitorController.ts",
    ].map((file) =>
      readFile(path.join(root, "src/tui/controllers", file), "utf8"),
    ),
  );
  for (const source of controllers) {
    assert.ok(!source.includes('from "@opentui/core"'));
    assert.ok(!source.includes('import("@opentui/core")'));
    assert.ok(!source.includes("export interface"));
  }
});

test("role selection renders CQRS workspace continuity without infrastructure access", async () => {
  const source = await readFile(
    path.join(process.cwd(), "src/tui/screens/RoleRunSelectionScreen.tsx"),
    "utf8",
  );
  assert.match(source, /getWorkspaceContinuity/);
  assert.match(source, /compatible-continue/);
  assert.match(source, /confirmDiscardRetained/);
  assert.match(source, /\[y\] Confirm/);
  assert.match(source, /for roles:/);
  assert.match(source, /Cancel \(default\)/);
  assert.match(source, /Retained workspace is leased · Esc Cancel/);
  assert.ok(!source.includes("node:fs"));
  assert.ok(!source.includes("node:child_process"));
  assert.ok(!source.includes("state.db"));
});

test("one worker monitor screen owns the selected-file SplitDiff seam", async () => {
  const root = process.cwd();
  const [screen, runScreen, splitDiff, eventLog] = await Promise.all([
    readFile(
      path.join(root, "src/tui/components/WorkerMonitorScreen.tsx"),
      "utf8",
    ),
    readFile(path.join(root, "src/tui/screens/RunScreen.tsx"), "utf8"),
    readFile(path.join(root, "src/tui/components/SplitDiff.tsx"), "utf8"),
    readFile(path.join(root, "src/tui/components/AgentEventLog.tsx"), "utf8"),
  ]);
  assert.equal(
    (screen.match(/export function WorkerMonitorScreen/g) ?? []).length,
    1,
  );
  assert.match(screen, /props\.fileDiffExpanded\s*&&\s*\(/);
  assert.equal((screen.match(/props\.roles\.map/g) ?? []).length, 1);
  assert.match(screen, /Logs \(\$\{props\.events\.length\}\)/);
  assert.match(screen, /Files \(\$\{props\.changedFiles\.length\}\)/);
  assert.match(eventLog, /ScrollBoxRenderable/);
  assert.match(eventLog, /scrollbarOptions/);
  assert.ok(!runScreen.includes("<SplitDiff"));
  assert.match(runScreen, /event\.name === "v"/);
  assert.doesNotMatch(runScreen, /event\.name === "r"/);
  assert.match(screen, /1 Roles {2}\| {2}2 Logs {2}\| {2}3 Files/);
  assert.match(screen, /Enter Open {2}\| {2}Ctrl\+C Cancel/);
  assert.match(screen, /r.*Resume/);
  assert.ok(splitDiff.includes("Select a changed file to view its diff."));
  assert.match(splitDiff, /event\.name === "up"/);
  assert.match(splitDiff, /event\.name === "down"/);
  assert.match(splitDiff, /number \| `\$\{number\}%`/);
  assert.match(screen, /patch lines/);
  assert.match(screen, /height="100%"/);
  assert.ok(splitDiff.includes("return () =>"));
});
