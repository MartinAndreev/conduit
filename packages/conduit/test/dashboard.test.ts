import { test } from "bun:test";
import assert from "node:assert/strict";
import {
  extractPatch,
  extractAppliedPatch,
  formatDashboard,
  splitPatchFiles,
  summarizeTranscript,
} from "../src/tui/dashboard.js";
import type { Run } from "../src/domains/runs/types/run.js";

test("dashboard keeps raw agent output collapsed until expanded", () => {
  const run: Run = {
    id: "test-run-1",
    featureId: "001",
    status: "running",
    createdAt: "2025-01-01T00:00:00Z",
    roles: [
      {
        name: "researcher",
        runner: "opencode",
        readOnly: true,
        owns: [],
        dependsOn: [],
        promptFile: "",
        prompt: "",
        command: "",
        args: [],
        skillSource: "",
        status: "completed",
      },
    ],
  };
  assert.doesNotMatch(
    formatDashboard({ run, selectedIndex: 0, expandedLog: undefined }),
    /Large source code output/,
  );
  assert.match(
    formatDashboard({
      run,
      selectedIndex: 0,
      expandedLog: "Large source code output",
    }),
    /Large source code output/,
  );
});

test("dashboard splits a patch into selectable file previews", () => {
  const files = splitPatchFiles(
    "diff --git a/a.ts b/a.ts\n-a\n+b\ndiff --git a/b.ts b/b.ts\n-c\n+d\n",
  );
  assert.deepEqual(
    files.map((file) => file.name),
    ["a.ts", "b.ts"],
  );
});

test("dashboard de-duplicates repeated transcript patch files", () => {
  const files = splitPatchFiles(
    "diff --git a/a.ts b/a.ts\n-old\n+new\ndiff --git a/a.ts b/a.ts\n-old-again\n+new-again\n",
  );
  assert.equal(files.length, 1);
  assert.match(files[0].diff, /new-again/);
});

test("dashboard transcript summaries collapse long output into one event line", () => {
  const summary = summarizeTranscript("exec\npnpm test\nline one\nline two\n");
  assert.equal(summary.command, "Ran pnpm test");
  assert.match(summary.detail, /4 lines captured/);
});

test("dashboard prioritizes the selected worktree diff over an agent report", () => {
  const run: Run = {
    id: "test-run-2",
    featureId: "001",
    status: "completed",
    createdAt: "2025-01-01T00:00:00Z",
    roles: [
      {
        name: "documentation",
        runner: "opencode",
        readOnly: false,
        owns: ["docs", "README.md"],
        dependsOn: [],
        promptFile: "",
        prompt: "",
        command: "",
        args: [],
        skillSource: "",
        status: "completed",
      },
    ],
  };
  const view = formatDashboard({
    run,
    selectedIndex: 0,
    selectedHasWorktree: true,
    selectedPatch: "diff --git a/README.md b/README.md\n-old\n+new\n",
  });
  assert.match(view, /Edited 1 file/);
  assert.match(view, /worktree diff/);
});

test("dashboard extracts a unified patch from a captured transcript", () => {
  const patch = extractPatch(
    "analysis\ndiff --git a/a.ts b/a.ts\n-const a = 1\n+const a = 2\n",
  );
  assert.match(patch!, /^diff --git/);
});

test("architect patch extraction ignores diffs observed before the applied patch", () => {
  const patch = extractAppliedPatch(
    "exec\ngit diff\ndiff --git a/old.js b/old.js\n-old\n+old\napply patch\npatch: completed\ndiff --git a/spec.md b/spec.md\n-old\n+new\nanalysis\n",
  );
  assert.match(patch!, /a\/spec\.md/);
  assert.doesNotMatch(patch!, /a\/old\.js/);
});
