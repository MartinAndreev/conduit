import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { localSpecKitRefinementPrompt } from "@domains/features/providers/local-spec-kit-refinement-prompt.js";
import { architectExecutionContract } from "@domains/refinement/types/architect-execution-contract.js";

const feature = {
  id: "042",
  directory: path.join("project", "specs", "042-example"),
};

test("refinement prompt requests compact packet artifacts instead of meta-prompts", () => {
  const prompt = localSpecKitRefinementPrompt(
    feature,
    "Operators can retry a failed export.",
    "OWNERSHIP POLICY\n\nPROJECT GUIDANCE (ADVISORY)",
  );

  for (const artifact of ["spec.md", "plan.md", "tasks.md", "test-cases.md"])
    assert.match(prompt, new RegExp(artifact.replace(".", "\\.")));
  assert.match(prompt, /only necessary files under .*contracts/);
  assert.match(
    prompt,
    /never write an architect prompt, mandate, role description/,
  );
  assert.match(prompt, /tables and short bullets/);
  assert.match(prompt, /pseudocode or signatures only when they remove/);
  assert.match(prompt, /OWNERSHIP POLICY/);
  assert.match(prompt, /PROJECT GUIDANCE \(ADVISORY\)/);
  assert.ok(
    prompt.split("\n").length < 60,
    "runtime prompt should stay compact",
  );
});

test("refinement prompt preserves clarification decisions and fails closed", () => {
  const prompt = localSpecKitRefinementPrompt(feature, "A feature request");

  assert.match(
    prompt,
    /clarifications\.md.*recorded answers are product decisions/,
  );
  assert.match(
    prompt,
    /If a material decision remains unclear after investigation/,
  );
  assert.match(prompt, /stop and write only .*questions\.md/);
  assert.match(prompt, /Do not update the handoff until answered/);
  assert.match(prompt, /application settings, not project guidance/);
  assert.match(prompt, /regardless of provider/);
  assert.match(prompt, /Project-authored architect guidance.*advisory/s);
});

test("higher effort and detail require a concrete language-independent design", () => {
  const policy = architectExecutionContract({
    effort: "exhaustive",
    detailLevel: "implementation-blueprint",
  });

  assert.match(
    policy,
    /Challenge the preferred design against credible alternatives/,
  );
  assert.match(policy, /concurrency modes, trust boundaries, migrations/);
  assert.match(policy, /concrete blueprint: component responsibilities/);
  assert.match(policy, /ordered control and data flow, state lifecycle/);
  assert.match(policy, /simple cohesive components, explicit boundaries/);
  assert.match(policy, /safe retry or idempotency/);
  assert.match(policy, /least privilege, backward compatibility/);
  assert.match(
    policy,
    /Do not introduce abstractions, patterns, or extensibility/,
  );
  assert.match(policy, /regardless of provider/);
});

test("standard concise policy remains small but design-complete", () => {
  const policy = architectExecutionContract({
    effort: "standard",
    detailLevel: "concise",
  });

  assert.match(policy, /smallest sound design/);
  assert.match(policy, /essential contracts, acceptance criteria/);
  assert.doesNotMatch(policy, /Challenge the preferred design/);
  assert.doesNotMatch(policy, /Provide a concrete blueprint/);
  assert.ok(policy.split("\n").length < 15);
});
