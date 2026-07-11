import { readFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";
import type { Config } from "../domains/configuration/types/config.js";
import type { Run } from "../domains/runs/types/run.js";

function summarizeLog(log: string): string {
  const tail = log.trim().split("\n").slice(-16).join("\n");
  return tail.length > 1800
    ? `${tail.slice(-1800)}\n…`
    : tail || "No captured output yet.";
}

interface PatchFile {
  name: string;
  diff: string;
}

interface FormatDashboardParams {
  run: Run;
  selectedIndex: number;
  selectedLog?: string;
  expandedLog?: string;
  patchFiles?: PatchFile[];
  fileIndex?: number;
  selectedPatch?: string;
  selectedTranscriptPatch?: string;
  selectedHasWorktree?: boolean;
}

export function formatDashboard({
  run,
  selectedIndex,
  selectedLog = "",
  expandedLog,
  patchFiles = [],
  fileIndex = 0,
  selectedPatch,
  selectedTranscriptPatch,
  selectedHasWorktree = false,
}: FormatDashboardParams): string {
  const lines = [
    "Conduit · Agent dashboard",
    `Feature ${run.featureId}  ·  ${run.status}`,
    "",
    "↑/↓ select  ·  Enter expand/collapse  ·  q quit",
    "",
  ];
  run.roles.forEach((role, index) => {
    const selected = index === selectedIndex ? "›" : " ";
    const state =
      role.status === "completed" ? "✓" : role.status === "failed" ? "×" : "○";
    const expanded = index === selectedIndex && expandedLog !== undefined;
    lines.push(
      `${selected} ${state} ${role.name.padEnd(12)} ${role.runner.padEnd(9)} ${expanded ? "Hide details" : "Show details"}`,
    );
    if (index === selectedIndex && !expanded) {
      if (selectedPatch) {
        const files = (selectedPatch.match(/^diff --git /gm) ?? []).length;
        lines.push(`    • Edited ${files} file${files === 1 ? "" : "s"}`);
        lines.push("      └ actual worktree diff · Enter to preview");
      } else if (selectedTranscriptPatch) {
        const files = (selectedTranscriptPatch.match(/^diff --git /gm) ?? [])
          .length;
        lines.push(
          `    • Applied patch with ${files} file${files === 1 ? "" : "s"}`,
        );
        lines.push("      └ captured architect patch · Enter to preview");
      } else if (selectedHasWorktree) {
        lines.push("    • No worktree changes detected");
        lines.push("      └ agent report available · Enter to view tail");
      } else {
        const summary = summarizeTranscript(selectedLog);
        lines.push(`    • ${summary.command}`);
        lines.push(`      └ ${summary.detail}`);
      }
    }
    if (expanded) {
      if (patchFiles.length) {
        const start = Math.max(
          0,
          Math.min(fileIndex - 3, patchFiles.length - 8),
        );
        const visibleFiles = patchFiles.slice(start, start + 8);
        lines.push(
          `    ── patch files ${fileIndex + 1}/${patchFiles.length} (↑/↓ browse · Enter collapse) ──`,
        );
        if (start > 0)
          lines.push(`    ↑ ${start} earlier file${start === 1 ? "" : "s"}`);
        visibleFiles.forEach((file, idx) => {
          const actualIndex = start + idx;
          lines.push(
            `    ${actualIndex === fileIndex ? "›" : " "} ${file.name}`,
          );
        });
        const remaining = patchFiles.length - (start + visibleFiles.length);
        if (remaining > 0)
          lines.push(
            `    ↓ ${remaining} more file${remaining === 1 ? "" : "s"}`,
          );
        return;
      }
      lines.push("    ── raw agent output (tail) ──");
      lines.push(
        ...summarizeLog(expandedLog ?? "")
          .split("\n")
          .map((line) => `    ${line}`),
      );
    }
  });
  return lines.join("\n");
}

export function extractPatch(log: string): string | undefined {
  const start = log.indexOf("diff --git ");
  return start >= 0 ? log.slice(start).trim() : undefined;
}

export function extractAppliedPatch(log: string): string | undefined {
  const completedPatch = log.lastIndexOf("patch: completed");
  if (completedPatch < 0) return undefined;
  const afterPatch = log.slice(completedPatch);
  const start = afterPatch.indexOf("diff --git ");
  if (start < 0) return undefined;
  const patch = afterPatch.slice(start);
  const nextActivity = patch.search(/\n(?:analysis|codex|exec|apply patch)\n/);
  return (nextActivity >= 0 ? patch.slice(0, nextActivity) : patch).trim();
}

export function splitPatchFiles(patch: string): PatchFile[] {
  const files = patch
    .split(/(?=^diff --git )/m)
    .filter(Boolean)
    .map((diff) => ({
      name: diff.match(/^diff --git a\/(.+?) b\//m)?.[1] ?? "unknown file",
      diff,
    }));
  return [...new Map(files.map((file) => [file.name, file])).values()];
}

export function summarizeTranscript(log: string): {
  command: string;
  detail: string;
} {
  const command = [...log.matchAll(/(?:^|\n)exec\n([^\n]+)/g)].at(-1)?.[1];
  const lines = log.trim().split("\n").filter(Boolean).length;
  const patch = extractPatch(log);
  const files = patch ? (patch.match(/^diff --git /gm) ?? []).length : 0;
  return {
    command: command ? `Ran ${command}` : "Captured agent activity",
    detail: patch
      ? `${files} changed file${files === 1 ? "" : "s"} · Enter to preview patch`
      : `${lines} lines captured · Enter to view tail`,
  };
}

async function readRoleLog(
  projectRoot: string,
  config: Config,
  run: Run,
  role: Run["roles"][number],
): Promise<string> {
  return readFile(
    path.join(projectRoot, config.stateDir, "runs", run.id, `${role.name}.log`),
    "utf8",
  ).catch(() => "No captured output yet.");
}

function readRolePatch(role: Run["roles"][number]): string | undefined {
  if (!role.worktree) return undefined;
  const result = spawnSync(
    "git",
    ["-C", role.worktree, "diff", "--no-ext-diff", "--unified=3", "HEAD"],
    { encoding: "utf8" },
  );
  return result.status === 0 && result.stdout.trim()
    ? result.stdout.trim()
    : undefined;
}

export async function startDashboard({
  projectRoot,
  config,
  runs,
  selectedRunId,
}: {
  projectRoot: string;
  config: Config;
  runs: Run[];
  selectedRunId?: string;
}): Promise<void> {
  if (!process.stdin.isTTY || !process.stdout.isTTY)
    throw new Error(
      "The dashboard requires an interactive terminal. Use `conduit status` for plain output.",
    );
  const run =
    runs.find((candidate) => candidate.id === selectedRunId) ?? runs[0];
  if (!run) return;
  const {
    createCliRenderer,
    TextRenderable,
    DiffRenderable,
    ScrollBarRenderable,
  } = await import("@opentui/core");
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  let selectedIndex = 0;
  let selectedLog = await readRoleLog(
    projectRoot,
    config,
    run,
    run.roles[selectedIndex],
  );
  let selectedPatch = readRolePatch(run.roles[selectedIndex]);
  let selectedTranscriptPatch =
    run.roles[selectedIndex]?.name === "architect"
      ? extractAppliedPatch(selectedLog)
      : extractPatch(selectedLog);
  let expandedLog: string | undefined;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let patch: any;
  let patchFiles: PatchFile[] = [];
  let fileIndex = 0;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let diff: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let resolveClosed: any;
  const closed = new Promise<void>((resolve) => {
    resolveClosed = resolve;
  });
  let syncingScrollbar = false;
  const patchViewportSize = 8;
  const text = new TextRenderable(renderer, {
    id: "conduit-dashboard",
    content: formatDashboard({
      run,
      selectedIndex,
      selectedLog,
      expandedLog,
      patchFiles,
      fileIndex,
      selectedPatch,
      selectedTranscriptPatch,
      selectedHasWorktree: Boolean(run.roles[selectedIndex]?.worktree),
    }),
    selectable: false,
    fg: "#D8D5C8",
    position: "absolute",
    left: 2,
    top: 1,
  });
  const refresh = () => {
    text.content = formatDashboard({
      run,
      selectedIndex,
      selectedLog,
      expandedLog,
      patchFiles,
      fileIndex,
      selectedPatch,
      selectedTranscriptPatch,
      selectedHasWorktree: Boolean(run.roles[selectedIndex]?.worktree),
    });
    if (patch) {
      if (!diff) {
        diff = new DiffRenderable(renderer, {
          id: "conduit-patch",
          diff: patch,
          view: "unified",
          width: "94%",
          height: "65%",
          position: "absolute",
          left: 2,
          top: run.roles.length + patchFiles.length + 8,
          showLineNumbers: true,
        });
        renderer.root.add(diff);
      } else {
        diff.diff = patch;
        diff.visible = true;
      }
    } else if (diff) diff.visible = false;
    const start = Math.max(
      0,
      Math.min(fileIndex - 3, patchFiles.length - patchViewportSize),
    );
    syncingScrollbar = true;
    patchScrollbar.scrollSize = patchFiles.length;
    patchScrollbar.viewportSize = patchViewportSize;
    patchScrollbar.scrollPosition = start;
    patchScrollbar.visible = patchFiles.length > patchViewportSize;
    syncingScrollbar = false;
  };
  const close = () => {
    renderer.keyInput.off("keypress", onKeypress);
    renderer.destroy();
    resolveClosed();
  };
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const onKeypress = async (key: any) => {
    if ((key.ctrl && key.name === "c") || key.name === "q") return close();
    if (patchFiles.length && key.name === "up") {
      fileIndex = Math.max(0, fileIndex - 1);
      patch = patchFiles[fileIndex].diff;
      return refresh();
    }
    if (patchFiles.length && key.name === "down") {
      fileIndex = Math.min(patchFiles.length - 1, fileIndex + 1);
      patch = patchFiles[fileIndex].diff;
      return refresh();
    }
    if (key.name === "up") {
      selectedIndex = Math.max(0, selectedIndex - 1);
      selectedLog = await readRoleLog(
        projectRoot,
        config,
        run,
        run.roles[selectedIndex],
      );
      selectedPatch = readRolePatch(run.roles[selectedIndex]);
      selectedTranscriptPatch =
        run.roles[selectedIndex]?.name === "architect"
          ? extractAppliedPatch(selectedLog)
          : extractPatch(selectedLog);
      expandedLog = undefined;
      patch = undefined;
      patchFiles = [];
      return refresh();
    }
    if (key.name === "down") {
      selectedIndex = Math.min(run.roles.length - 1, selectedIndex + 1);
      selectedLog = await readRoleLog(
        projectRoot,
        config,
        run,
        run.roles[selectedIndex],
      );
      selectedPatch = readRolePatch(run.roles[selectedIndex]);
      selectedTranscriptPatch =
        run.roles[selectedIndex]?.name === "architect"
          ? extractAppliedPatch(selectedLog)
          : extractPatch(selectedLog);
      expandedLog = undefined;
      patch = undefined;
      patchFiles = [];
      return refresh();
    }
    if (key.name === "return" || key.name === "space") {
      expandedLog = expandedLog === undefined ? selectedLog : undefined;
      patchFiles =
        expandedLog === undefined
          ? []
          : splitPatchFiles(selectedPatch ?? selectedTranscriptPatch ?? "");
      fileIndex = 0;
      patch = patchFiles[0]?.diff;
      refresh();
    }
  };
  const patchScrollbar = new ScrollBarRenderable(renderer, {
    id: "conduit-patch-scrollbar",
    orientation: "vertical",
    height: 12,
    position: "absolute",
    right: 2,
    top: 6,
    showArrows: true,
    trackOptions: { foregroundColor: "#8FB6A0", backgroundColor: "#2B332A" },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    onChange: (position: any) => {
      if (syncingScrollbar || !patchFiles.length) return;
      fileIndex = Math.min(
        patchFiles.length - 1,
        Math.max(0, Math.round(position) + 3),
      );
      patch = patchFiles[fileIndex].diff;
      refresh();
    },
  });
  patchScrollbar.visible = false;
  renderer.root.add(text);
  renderer.root.add(patchScrollbar);
  renderer.keyInput.on("keypress", onKeypress);
  return closed;
}
