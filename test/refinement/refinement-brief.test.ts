import assert from "node:assert/strict";
import test from "node:test";
import {
  formatRefinementBrief,
  parseRefinementBrief,
} from "../../src/helpers/formatting/refinement-brief.js";

test("refinement brief writes durable, clearly separated story sections", () => {
  const story = formatRefinementBrief({
    problem: "Operators cannot understand failed runs.",
    audience: "Platform operators",
    outcome: "Show actionable failure information.",
    constraints: "Do not expose secrets.",
    guidelines: "Use the existing theme and preserve keyboard navigation.",
  });

  assert.match(story, /^## Problem \/ user story/m);
  assert.match(story, /^## Implementation and design guidance/m);
  assert.doesNotMatch(story, /Architect effort|Architect detail level/);
});

test("refinement brief round-trips optional guidance without execution preferences", () => {
  const parsed = parseRefinementBrief(`## Problem / user story

Build a monitor.

## User or audience

Operators

## Desired outcome and acceptance criteria

Failures are actionable.

## Implementation and design guidance

Use existing theme tokens.

`);

  assert.equal(parsed.guidelines, "Use existing theme tokens.");
  assert.equal(parsed.problem, "Build a monitor.");
  assert.equal(parsed.audience, "Operators");
});

test("legacy label-based stories retain their content", () => {
  const parsed = parseRefinementBrief(
    "Problem: Build a monitor\n\nUser: Operators\n\nDesired outcome: It works",
  );
  assert.equal(parsed.problem, "Build a monitor");
  assert.equal(parsed.audience, "Operators");
});
