import { test } from "bun:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { FileRefinementRevisionRepository } from "@domains/refinement/repositories/file-revision-repository.js";
import { createStartArchitectRefinementHandler } from "@domains/refinement/handlers/start-architect-refinement-handler.js";
import { createSubmitArchitectAnswersHandler } from "@domains/refinement/handlers/submit-architect-answers-handler.js";
import { createReviewRefinementPacketHandler } from "@domains/refinement/handlers/review-refinement-packet-handler.js";

const config = {
  version: 1,
  specsDir: "specs",
  stateDir: ".conduit",
  roles: {},
};

test("architect clarification and packet review keep an auditable revision history", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "conduit-revision-test-"));
  const feature = {
    id: "001",
    directory: path.join(root, "specs", "001-revisions"),
  };
  try {
    await mkdir(feature.directory, { recursive: true });
    const repository = new FileRefinementRevisionRepository();
    let asksQuestions = true;
    const start = createStartArchitectRefinementHandler({
      projectRoot: root,
      loadConfig: async () => config,
      findFeature: async () => feature,
      refinementPrompt: () => "refine",
      revisionRepository: repository,
      runArchitect: async ({ logFile }) => {
        await mkdir(path.dirname(logFile), { recursive: true });
        await writeFile(logFile, "analysis\narchitect pass complete\n");
        if (asksQuestions)
          await writeFile(
            path.join(path.dirname(logFile), "questions.md"),
            "# Architect questions\n\n## Q-001\n\nWhich retention period should apply?\n\n### Options\n\n- 30 days\n- 90 days\n",
          );
        return { logFile };
      },
    });

    const first = await start({
      type: "startArchitectRefinement",
      featureId: "001",
      story: "A story",
    });
    assert.equal(first.success, true);
    if (!first.success) return;
    assert.equal(first.data.status, "awaiting_clarification");
    assert.equal(first.data.revisionId, "001");
    const questions = await repository.readQuestions(
      (await repository.getLatest(feature))!,
    );
    assert.equal(questions[0]?.id, "Q-001");
    assert.equal(
      questions[0]?.question,
      "Which retention period should apply?",
    );
    assert.deepEqual(questions[0]?.options, ["30 days", "90 days"]);
    assert.match(
      await readFile(
        path.join(feature.directory, "revisions", "001", "questions.md"),
        "utf8",
      ),
      /Q-001/,
    );

    const answers = createSubmitArchitectAnswersHandler({
      projectRoot: root,
      loadConfig: async () => config,
      findFeature: async () => feature,
      repository,
    });
    const answered = await answers({
      type: "submitArchitectAnswers",
      featureId: "001",
      revisionId: "001",
      answers: "Use 90 days.",
    });
    assert.equal(answered.success, true);
    assert.match(
      await readFile(path.join(feature.directory, "clarifications.md"), "utf8"),
      /Use 90 days/,
    );

    asksQuestions = false;
    const resumed = await start({
      type: "startArchitectRefinement",
      featureId: "001",
      story: "A story",
      revisionId: "001",
    });
    assert.equal(resumed.success, true);
    if (!resumed.success) return;
    assert.equal(resumed.data.status, "ready_for_review");

    const review = createReviewRefinementPacketHandler({
      projectRoot: root,
      loadConfig: async () => config,
      findFeature: async () => feature,
      repository,
    });
    const requested = await review({
      type: "reviewRefinementPacket",
      featureId: "001",
      revisionId: "001",
      decision: "changes_requested",
      feedback: "Make the retention decision explicit in acceptance criteria.",
    });
    assert.equal(requested.success, true);
    if (!requested.success) return;
    assert.equal(requested.data.nextRevisionId, "002");

    const approved = await review({
      type: "reviewRefinementPacket",
      featureId: "001",
      revisionId: "002",
      decision: "approved",
    });
    assert.equal(approved.success, true);
    assert.equal((await repository.getLatest(feature))?.status, "approved");
    assert.match(
      await readFile(
        path.join(feature.directory, "revisions", "001", "review.md"),
        "utf8",
      ),
      /changes_requested/,
    );
    assert.match(
      await readFile(
        path.join(feature.directory, "revisions", "002", "review.md"),
        "utf8",
      ),
      /approved/,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("failed architect startup is persisted for research resume", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "conduit-architect-fail-"));
  const feature = {
    id: "001",
    directory: path.join(root, "specs", "001-failed-architect"),
  };
  try {
    await mkdir(feature.directory, { recursive: true });
    const repository = new FileRefinementRevisionRepository();
    const start = createStartArchitectRefinementHandler({
      projectRoot: root,
      loadConfig: async () => config,
      findFeature: async () => feature,
      refinementPrompt: () => "refine",
      revisionRepository: repository,
      runArchitect: async () => {
        throw new Error("runner unavailable");
      },
    });

    const result = await start({
      type: "startArchitectRefinement",
      featureId: "001",
      story: "A researched story",
    });

    assert.equal(result.success, false);
    assert.equal((await repository.getLatest(feature))?.status, "failed");
    assert.equal(result.error?.message, "runner unavailable");
    await assert.rejects(
      readFile(
        path.join(feature.directory, "revisions", "001", "architect-run.md"),
        "utf8",
      ),
      { code: "ENOENT" },
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
