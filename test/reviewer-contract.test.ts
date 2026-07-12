import test from "node:test";
import assert from "node:assert/strict";
import { coreRoleContract } from "@domains/roles/assets/core-role-contract.js";
import { buildReviewPrompt } from "@domains/runs/handlers/final-review-handler.js";
import type { Run } from "@domains/runs/types/run.js";
import { commandForRole } from "@domains/runs/repositories/run-orchestrator.js";

test("reviewer contract is a comprehensive fail-closed production gate", () => {
  const contract = coreRoleContract("reviewer");

  for (const concern of [
    "correctness and regressions",
    "authorization, security, privacy",
    "concurrency, and resource-lifecycle",
    "migrations, and rollback safety",
    "unbounded work",
    "violations of repository conventions",
    "test quality, observability, configuration, and documentation",
  ])
    assert.match(contract, new RegExp(concern));
  assert.match(contract, /Reject when any material issue remains/);
  assert.match(contract, /do not demand speculative abstractions/);
});

test("final review prompt preserves complete packet and diff evidence", () => {
  const longEvidence = `boundary-marker-${"x".repeat(5000)}-final-marker`;
  const run = {
    id: "run-1",
    featureId: "042",
    status: "completed",
    roles: [],
  } as unknown as Run;
  const prompt = buildReviewPrompt(
    "042",
    run,
    new Map([["backend", longEvidence]]),
    longEvidence,
  );

  assert.equal(prompt.match(/final-marker/g)?.length, 2);
  assert.match(prompt, /complete approved packet, repository guidance/);
  assert.match(
    prompt,
    /performance, maintainability, compatibility, operability/,
  );
  assert.match(prompt, /Reject for any material/);
});

test("final reviewer execution uses configured runner, model, and effort", () => {
  const [command, args] = commandForRole(
    {
      runner: "opencode",
      model: "provider/review-model",
      effort: "high",
    },
    "/tmp/review-prompt.md",
  );

  assert.equal(command, "opencode");
  assert.deepEqual(args.slice(0, 3), [
    "run",
    "--model",
    "provider/review-model",
  ]);
  assert.match(args.at(-1) ?? "", /Requested reasoning effort: high/);
  assert.doesNotMatch(args.join(" "), /codex/);
});
