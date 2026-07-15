import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import type { Config } from "../src/domains/configuration/types/config.js";
import {
  architectProgressMessage,
  refineCommand,
} from "../src/domains/refinement/handlers/refine-command.js";

const stubConfig: Config = {
  version: 1,
  specsDir: "specs",
  stateDir: ".conduit",
  roles: {},
};

const stubProgress = async <T>(
  _text: string,
  work: (params?: { setText?: (text: string) => void }) => Promise<T>,
): Promise<T> => work({ setText: () => {} });

test("refine saves a story without spending an architect run by default", async () => {
  const output: string[] = [];
  let savedStory: string | undefined;
  let savedTestCases: string | undefined;
  const result = await refineCommand(
    "001",
    "Users need invitations",
    { project: "/tmp/demo", architect: false },
    {
      output: (line: string) => output.push(line),
      progress: stubProgress,
      loadConfig: async () => stubConfig,
      findFeature: async () => ({
        id: "001",
        directory: "/tmp/demo/specs/001-invitations",
      }),
      writeStory: async (
        _feature: { id: string; directory: string },
        story: string,
      ) => {
        savedStory = story;
        return "/tmp/demo/specs/001-invitations/story.md";
      },
      writeTestCases: async (
        _feature: { id: string; directory: string },
        testCases: string,
      ) => {
        savedTestCases = testCases;
        return "/tmp/demo/specs/001-invitations/test-cases.md";
      },
    },
  );
  assert.equal(savedStory, "Users need invitations");
  assert.equal(savedTestCases, "");
  assert.equal((result as { architectRan: boolean }).architectRan, false);
  assert.match(output[2]!, /Run again with --architect/);
});

test("architect refinement uses an existing story without reopening the form", async () => {
  let architectPrompt: string | undefined;
  const result = await refineCommand(
    "001",
    undefined,
    { project: "/tmp/demo", architect: true },
    {
      output: () => {},
      progress: stubProgress,
      loadConfig: async () => stubConfig,
      findFeature: async () => ({
        id: "001",
        directory: "/tmp/demo/specs/001-invitations",
      }),
      readStory: async () => "Saved story",
      refinementPrompt: (
        _feature: { id: string; directory: string },
        story: string,
      ) => `refine: ${story}`,
      runArchitect: async ({
        prompt,
        logFile,
      }: {
        prompt: string;
        logFile: string;
      }) => {
        architectPrompt = prompt;
        return { logFile };
      },
    },
  );
  assert.equal(architectPrompt, "refine: Saved story");
  assert.equal((result as { architectRan: boolean }).architectRan, true);
  assert.equal(
    (result as { storyFile: string | undefined }).storyFile,
    undefined,
  );
});

test("architect progress hides raw output but reports the active command", () => {
  assert.equal(
    architectProgressMessage("analysis\nexec\npnpm test --filter api\n"),
    "Architect is running: pnpm test --filter api",
  );
  assert.equal(
    architectProgressMessage("apply patch\n"),
    "Architect is applying the specification patch",
  );
  assert.equal(
    architectProgressMessage(
      `${JSON.stringify({
        type: "item.completed",
        item: {
          type: "reasoning",
          text: "Checking the packet contracts before editing",
        },
      })}\n`,
    ),
    "Architect reasoning: Checking the packet contracts before editing",
  );
  assert.equal(
    architectProgressMessage(
      `${JSON.stringify({
        type: "item.started",
        item: {
          type: "command_execution",
          command: "pnpm test --filter refinement",
        },
      })}\n`,
    ),
    "Architect is running: pnpm test --filter refinement",
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
        progress: stubProgress,
        loadConfig: async () => stubConfig,
        findFeature: async () => feature,
        refinementPrompt: () => "refine",
        writeStory: async () => "story.md",
        writeTestCases: async () => "test-cases.md",
        collectArchitectAnswers: async (questions: string) => {
          assert.match(questions, /Which audience/);
          return "Operators first.";
        },
        runArchitect: async ({ logFile }) => {
          calls += 1;
          if (calls === 1) {
            await mkdir(path.dirname(logFile), { recursive: true });
            await writeFile(
              path.join(path.dirname(logFile), "questions.md"),
              "# Questions\n\n- Q1: Which audience?",
            );
          }
          return { logFile: "architect.log" };
        },
      },
    );
    assert.equal(calls, 2);
    assert.equal(
      (result as unknown as { architectRan: boolean }).architectRan,
      true,
    );
    assert.ok(
      /Operators first/.test(
        await readFile(
          path.join(feature.directory, "clarifications.md"),
          "utf8",
        ),
      ),
    );
  } finally {
    await rm(project, { recursive: true, force: true });
  }
});
