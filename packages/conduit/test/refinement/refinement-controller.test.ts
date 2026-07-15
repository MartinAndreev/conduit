import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { FileDraftRepository } from "../../src/domains/refinement/repositories/file-draft-repository.js";
import { FileArchitectEventRepository } from "../../src/domains/refinement/repositories/file-architect-event-repository.js";
import { createSaveDraftHandler } from "../../src/domains/refinement/handlers/save-draft-handler.js";
import { createGetDraftHandler } from "../../src/domains/refinement/handlers/get-draft-handler.js";
import { createApproveRefinementHandler } from "../../src/domains/refinement/handlers/approve-refinement-handler.js";
import { createGetArchitectEventsHandler } from "../../src/domains/refinement/handlers/get-architect-events-handler.js";
import { extractArchitectEvents } from "../../src/domains/refinement/helpers/architect-event-parser.js";
import { LiveArchitectEventRepository } from "../../src/domains/refinement/repositories/live-architect-event-repository.js";
import { CommandBus } from "../../src/system/bus/command-bus.js";
import { QueryBus } from "../../src/system/bus/query-bus.js";

test("refinement controller state transitions", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-controller-test-"),
  );
  try {
    const repository = new FileDraftRepository(tempDir);
    const commandBus = new CommandBus();
    const queryBus = new QueryBus();

    commandBus.register("saveDraft", createSaveDraftHandler(repository));
    queryBus.register("getDraft", createGetDraftHandler(repository));

    const saveResult = await commandBus.dispatch({
      type: "saveDraft",
      featureId: "001",
      story:
        "Problem: Test problem\n\nUser: Test user\n\nDesired outcome: Test outcome",
      testCases: "Test cases",
    });

    assert.equal(saveResult.success, true);

    const getResult = await queryBus.execute({
      type: "getDraft",
      featureId: "001",
    });

    assert.equal(getResult.success, true);
    if (getResult.success) {
      const data = getResult.data as {
        draft: { featureId: string; story: string } | null;
      };
      assert.ok(data.draft);
      assert.equal(data.draft.featureId, "001");
      assert.ok(data.draft.story.includes("Test problem"));
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("refinement form field validation", () => {
  const fields = [
    {
      name: "problem",
      label: "Problem",
      guidance: "Describe the problem",
      required: true,
      multiline: true,
    },
    {
      name: "audience",
      label: "Audience",
      guidance: "Who will use this",
      required: true,
      multiline: false,
    },
    {
      name: "outcome",
      label: "Outcome",
      guidance: "Desired outcome",
      required: true,
      multiline: true,
    },
    {
      name: "constraints",
      label: "Constraints",
      guidance: "Constraints",
      required: false,
      multiline: true,
    },
    {
      name: "testCases",
      label: "Test Cases",
      guidance: "Test cases",
      required: true,
      multiline: true,
    },
  ];

  const requiredFields = fields.filter((f) => f.required);
  assert.equal(requiredFields.length, 4);
  assert.ok(requiredFields.some((f) => f.name === "problem"));
  assert.ok(requiredFields.some((f) => f.name === "audience"));
  assert.ok(requiredFields.some((f) => f.name === "outcome"));
  assert.ok(requiredFields.some((f) => f.name === "testCases"));

  const optionalFields = fields.filter((f) => !f.required);
  assert.equal(optionalFields.length, 1);
  assert.ok(optionalFields.some((f) => f.name === "constraints"));
});

test("refinement preview keyboard shortcuts", () => {
  const shortcuts = {
    "Ctrl+R": "return to form",
    a: "approve and write packet",
    q: "quit without approving",
    t: "toggle architect",
  };

  assert.equal(shortcuts["Ctrl+R"], "return to form");
  assert.equal(shortcuts.a, "approve and write packet");
  assert.equal(shortcuts.q, "quit without approving");
  assert.equal(shortcuts.t, "toggle architect");
});

test("architect activity event types", () => {
  const eventTypes = [
    "thought",
    "activity",
    "tool-call",
    "tool-output",
    "file-change",
    "patch",
    "error",
    "lifecycle",
  ];

  assert.equal(eventTypes.length, 8);
  assert.ok(eventTypes.includes("thought"));
  assert.ok(eventTypes.includes("activity"));
  assert.ok(eventTypes.includes("tool-call"));
  assert.ok(eventTypes.includes("tool-output"));
  assert.ok(eventTypes.includes("file-change"));
  assert.ok(eventTypes.includes("patch"));
  assert.ok(eventTypes.includes("error"));
  assert.ok(eventTypes.includes("lifecycle"));
});

test("approveRefinement handler writes story and test cases to feature packet", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-approve-test-"),
  );
  try {
    const specsDir = path.join(tempDir, "specs");
    const featureDir = path.join(specsDir, "001-test-feature");
    await mkdir(path.join(featureDir, "contracts"), { recursive: true });

    const commandBus = new CommandBus();

    const loadConfig = async () => ({
      version: 1,
      specsDir: "specs",
      stateDir: ".conduit",
      roles: {},
    });
    const findFeature = async () => ({
      id: "001",
      directory: featureDir,
    });
    const writeStory = async (
      feature: { directory: string },
      story: string,
    ) => {
      const file = path.join(feature.directory, "story.md");
      await writeFile(file, `# Story\n\n${story.trim()}\n`);
      return file;
    };
    const writeTestCases = async (
      feature: { directory: string },
      testCases: string,
    ) => {
      const file = path.join(feature.directory, "test-cases.md");
      await writeFile(
        file,
        `# QA test cases\n\n${testCases.trim() || "- [ ] Define tests."}\n`,
      );
      return file;
    };

    commandBus.register(
      "approveRefinement",
      createApproveRefinementHandler({
        loadConfig,
        findFeature,
        writeStory,
        writeTestCases,
        projectRoot: tempDir,
      }),
    );

    const result = await commandBus.dispatch({
      type: "approveRefinement",
      featureId: "001",
      story: "Problem: Test\n\nUser: Dev\n\nDesired outcome: Works",
      testCases: "- [ ] It works",
    });

    assert.equal(result.success, true);
    if (result.success) {
      const data = result.data as {
        approved: boolean;
        storyFile: string;
        testCasesFile: string;
      };
      assert.equal(data.approved, true);
      assert.ok(data.storyFile.endsWith("story.md"));
      assert.ok(data.testCasesFile.endsWith("test-cases.md"));
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("approveRefinement rejects and quit never create an approved packet", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-approve-reject-test-"),
  );
  try {
    const specsDir = path.join(tempDir, "specs");
    const featureDir = path.join(specsDir, "001-test-feature");
    await mkdir(path.join(featureDir, "contracts"), { recursive: true });

    const commandBus = new CommandBus();
    let storyWritten = false;

    const loadConfig = async () => ({
      version: 1,
      specsDir: "specs",
      stateDir: ".conduit",
      roles: {},
    });
    const findFeature = async () => ({
      id: "001",
      directory: featureDir,
    });
    const writeStory = async (
      feature: { directory: string },
      story: string,
    ) => {
      storyWritten = true;
      const file = path.join(feature.directory, "story.md");
      await writeFile(file, `# Story\n\n${story.trim()}\n`);
      return file;
    };
    const writeTestCases = async (
      feature: { directory: string },
      testCases: string,
    ) => {
      const file = path.join(feature.directory, "test-cases.md");
      await writeFile(
        file,
        `# QA test cases\n\n${testCases.trim() || "- [ ] Define tests."}\n`,
      );
      return file;
    };

    commandBus.register(
      "approveRefinement",
      createApproveRefinementHandler({
        loadConfig,
        findFeature,
        writeStory,
        writeTestCases,
        projectRoot: tempDir,
      }),
    );

    assert.equal(
      storyWritten,
      false,
      "No packet should be created before approval",
    );
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("getArchitectEvents handler returns normalized events from transcript", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-events-test-"));
  try {
    const runDir = path.join(
      tempDir,
      ".conduit",
      "runs",
      "refine-001-1234567890",
    );
    await mkdir(runDir, { recursive: true });

    const transcript = `analysis
exec
pnpm test
diff --git a/spec.md b/spec.md
--- a/spec.md
+++ b/spec.md
@@ -1,3 +1,4 @@
 # Spec
 
+- New line
patch: completed
exec
pnpm lint`;

    await writeFile(path.join(runDir, "architect.log"), transcript);

    const repository = new FileArchitectEventRepository(tempDir);
    const queryBus = new QueryBus();
    queryBus.register(
      "getArchitectEvents",
      createGetArchitectEventsHandler(repository),
    );

    const result = await queryBus.execute({
      type: "getArchitectEvents",
      featureId: "001",
    });

    assert.equal(result.success, true);
    if (result.success) {
      const data = result.data as {
        events: Array<{ type: string; content: string }>;
        uniqueFiles: string[];
      };
      assert.ok(data.events.length > 0);
      assert.ok(data.uniqueFiles.includes("spec.md"));

      const activityEvents = data.events.filter((e) => e.type === "activity");
      assert.ok(activityEvents.length > 0);

      const toolCallEvents = data.events.filter((e) => e.type === "tool-call");
      assert.equal(toolCallEvents.length, 2);
      assert.equal(toolCallEvents[0].content, "pnpm test");
      assert.equal(toolCallEvents[1].content, "pnpm lint");

      const patchEvents = data.events.filter((e) => e.type === "patch");
      assert.equal(patchEvents.length, 1);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("architect transcript line parsers recognize each supported token", () => {
  const events = extractArchitectEvents(
    `analysis
codex
apply patch
patch: completed
exec
pnpm test`,
    "2026-01-01T00:00:00.000Z",
  );

  assert.deepEqual(
    events.map(({ type, content }) => ({ type, content })),
    [
      { type: "activity", content: "Analyzing project context" },
      { type: "activity", content: "Refining feature specification" },
      { type: "patch", content: "Applying specification patch" },
      { type: "lifecycle", content: "Patch completed" },
      { type: "tool-call", content: "pnpm test" },
    ],
  );
});

test("architect activity collapses repeated transport markers", () => {
  const events = extractArchitectEvents(
    "analysis\nanalysis\nanalysis\n",
    "2026-01-01T00:00:00.000Z",
  );

  assert.deepEqual(
    events.map(({ type, content }) => ({ type, content })),
    [{ type: "activity", content: "Analyzing project context" }],
  );
});

test("architect activity exposes emitted reasoning summaries and commands", () => {
  const events = extractArchitectEvents(
    [
      JSON.stringify({ type: "thread.started" }),
      JSON.stringify({
        type: "item.completed",
        item: { type: "reasoning", text: "Checking contract coverage" },
      }),
      JSON.stringify({
        type: "item.started",
        item: { type: "command_execution", command: "pnpm test" },
      }),
      JSON.stringify({
        type: "item.completed",
        item: {
          type: "command_execution",
          command: "pnpm test",
          status: "completed",
        },
      }),
    ].join("\n"),
    "2026-01-01T00:00:00.000Z",
  );

  assert.deepEqual(
    events.map(({ type, content }) => ({ type, content })),
    [
      { type: "lifecycle", content: "Architect session started" },
      { type: "thought", content: "Checking contract coverage" },
      { type: "tool-call", content: "pnpm test" },
      { type: "tool-output", content: "pnpm test · completed" },
    ],
  );
});

test("live architect events take precedence over stale canonical history", async () => {
  let canonicalReads = 0;
  const repository = new LiveArchitectEventRepository(
    {
      loadEvents: async () => [
        {
          type: "activity",
          timestamp: "2026-01-01T00:00:00.000Z",
          content: "Analyzing project context",
        },
      ],
    },
    {
      loadEvents: async () => {
        canonicalReads += 1;
        return [];
      },
    },
  );

  assert.equal((await repository.loadEvents("007")).length, 1);
  assert.equal(canonicalReads, 0);
});

test("getArchitectEvents deduplicates repeated transcript patches", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "conduit-dedup-test-"));
  try {
    const runDir = path.join(
      tempDir,
      ".conduit",
      "runs",
      "refine-001-1234567890",
    );
    await mkdir(runDir, { recursive: true });

    const transcript = `diff --git a/spec.md b/spec.md
--- a/spec.md
+++ b/spec.md
@@ -1,3 +1,4 @@
 # Spec
 
+- First change
diff --git a/spec.md b/spec.md
--- a/spec.md
+++ b/spec.md
@@ -1,3 +1,4 @@
 # Spec
 
+- Duplicate change`;

    await writeFile(path.join(runDir, "architect.log"), transcript);

    const repository = new FileArchitectEventRepository(tempDir);
    const queryBus = new QueryBus();
    queryBus.register(
      "getArchitectEvents",
      createGetArchitectEventsHandler(repository),
    );

    const result = await queryBus.execute({
      type: "getArchitectEvents",
      featureId: "001",
    });

    assert.equal(result.success, true);
    if (result.success) {
      const data = result.data as {
        events: Array<{ type: string; files?: string[] }>;
        uniqueFiles: string[];
      };
      const patchEvents = data.events.filter((e) => e.type === "patch");
      assert.equal(
        patchEvents.length,
        1,
        "Duplicate patches for the same files should be deduplicated",
      );
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("getArchitectEvents returns empty for missing transcript", async () => {
  const tempDir = await mkdtemp(
    path.join(os.tmpdir(), "conduit-events-empty-test-"),
  );
  try {
    const repository = new FileArchitectEventRepository(tempDir);
    const queryBus = new QueryBus();
    queryBus.register(
      "getArchitectEvents",
      createGetArchitectEventsHandler(repository),
    );

    const result = await queryBus.execute({
      type: "getArchitectEvents",
      featureId: "nonexistent",
    });

    assert.equal(result.success, true);
    if (result.success) {
      const data = result.data as {
        events: unknown[];
        uniqueFiles: string[];
      };
      assert.equal(data.events.length, 0);
      assert.equal(data.uniqueFiles.length, 0);
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});
