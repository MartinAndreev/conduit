import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {
  architectProgressMessage,
  refineCommand,
} from "../src/commands/refine.js";

test("refine saves a story without spending an architect run by default", async () => {
  const output = [];
  let savedStory;
  let savedTestCases;
  const result = await refineCommand(
    "001",
    "Users need invitations",
    { project: "/tmp/demo", architect: false },
    {
      output: (line) => output.push(line),
      progress: async (_text, work) => work({ setText: () => {} }),
      loadConfig: async () => ({}),
      findFeature: async () => ({
        id: "001",
        directory: "/tmp/demo/specs/001-invitations",
      }),
      writeStory: async (_feature, story) => {
        savedStory = story;
        return "/tmp/demo/specs/001-invitations/story.md";
      },
      writeTestCases: async (_feature, testCases) => {
        savedTestCases = testCases;
        return "/tmp/demo/specs/001-invitations/test-cases.md";
      },
    },
  );
  assert.equal(savedStory, "Users need invitations");
  assert.equal(savedTestCases, "");
  assert.equal(result.architectRan, false);
  assert.match(output[2], /Run again with --architect/);
});

test("architect refinement uses an existing story without reopening the form", async () => {
  let architectPrompt;
  const result = await refineCommand(
    "001",
    undefined,
    { project: "/tmp/demo", architect: true },
    {
      output: () => {},
      progress: async (_text, work) => work({ setText: () => {} }),
      loadConfig: async () => ({}),
      findFeature: async () => ({
        id: "001",
        directory: "/tmp/demo/specs/001-invitations",
      }),
      readStory: async () => "Saved story",
      refinementPrompt: (_feature, story) => `refine: ${story}`,
      runArchitect: async ({ prompt, logFile }) => {
        architectPrompt = prompt;
        return { logFile };
      },
    },
  );
  assert.equal(architectPrompt, "refine: Saved story");
  assert.equal(result.architectRan, true);
  assert.equal(result.storyFile, undefined);
});

test("architect progress hides raw output but reports the active command", () => {
  assert.equal(
    architectProgressMessage("analysis\nexec\npnpm test --filter api\n"),
    "Codex is running: pnpm test --filter api",
  );
  assert.equal(
    architectProgressMessage("apply patch\n"),
    "Codex is applying the specification patch",
  );
});

test("architect questions pause refinement, collect an answer, and resume", async () => {
  const project = await mkdtemp(path.join(os.tmpdir(), "conduit-refine-"));
  const feature = {
    id: "001",
    directory: path.join(project, "specs", "001-demo"),
  };
  await mkdir(feature.directory, { recursive: true });
  let calls = 0;
  try {
    const result = await refineCommand(
      "001",
      "Story",
      { project, architect: true },
      {
        output: () => {},
        progress: async (_text, work) => work({ setText: () => {} }),
        loadConfig: async () => ({}),
        findFeature: async () => feature,
        refinementPrompt: () => "refine",
        writeStory: async () => "story.md",
        writeTestCases: async () => "test-cases.md",
        collectArchitectAnswers: async (questions) => {
          assert.match(questions, /Which audience/);
          return "Operators first.";
        },
        runArchitect: async () => {
          calls += 1;
          if (calls === 1)
            await writeFile(
              path.join(feature.directory, "questions.md"),
              "# Questions\n\n- Q1: Which audience?",
            );
          return { logFile: "architect.log" };
        },
      },
    );
    assert.equal(calls, 2);
    assert.equal(result.architectRan, true);
    assert.match(
      await readFile(path.join(feature.directory, "clarifications.md"), "utf8"),
      /Operators first/,
    );
  } finally {
    await rm(project, { recursive: true, force: true });
  }
});
