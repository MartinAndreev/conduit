import assert from "node:assert/strict";
import { test } from "bun:test";
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

test("legacy Markdown headings populate their matching refinement fields", () => {
  const parsed = parseRefinementBrief(`## Problem

Build a monitor.

## User

Operators

## Acceptance criteria

- [ ] Failures are actionable.
`);

  assert.equal(parsed.problem, "Build a monitor.");
  assert.equal(parsed.audience, "Operators");
  assert.equal(parsed.outcome, "- [ ] Failures are actionable.");
});

test("multiline Markdown sections retain all paragraphs and checklist items", () => {
  const parsed = parseRefinementBrief(`# Story

## Problem / user story

The current payment flow uses a legacy provider.

The replacement must preserve legacy sessions while modernizing checkout.

## User or audience

Customers purchasing pro plans.

## Desired outcome and acceptance criteria

- [ ] Customers authenticate before checkout.
- [ ] Bank transfer instructions are emailed.
- [ ] Card payments redirect to Stripe.

## Implementation and design guidance

Use the established component library.
Never expose provider secrets.
`);

  assert.equal(
    parsed.problem,
    "The current payment flow uses a legacy provider.\n\nThe replacement must preserve legacy sessions while modernizing checkout.",
  );
  assert.equal(parsed.audience, "Customers purchasing pro plans.");
  assert.equal(
    parsed.outcome,
    "- [ ] Customers authenticate before checkout.\n- [ ] Bank transfer instructions are emailed.\n- [ ] Card payments redirect to Stripe.",
  );
  assert.equal(
    parsed.guidelines,
    "Use the established component library.\nNever expose provider secrets.",
  );
});
