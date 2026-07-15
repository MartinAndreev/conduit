import assert from "node:assert/strict";
import { test } from "bun:test";
import { parseQuestions } from "@domains/refinement/helpers/question-parser.js";

test("architect questions retain question, context, options, and unblocker", () => {
  const questions = parseQuestions(`# Architect questions

## Q-001 — Checkout cutover and legacy compatibility

### Question

Should existing checkout sessions remain valid during cutover?

### Why this matters

Invalidating sessions would interrupt buyers already in checkout.

### Context

The repository still exposes the legacy order controller.

### Options

- Preserve sessions until expiry — smoother migration
- Force restart — simpler implementation

### Smallest unblocker

Choose whether in-flight sessions survive deployment.
`);

  assert.equal(questions.length, 1);
  assert.equal(
    questions[0]?.question,
    "Should existing checkout sessions remain valid during cutover?",
  );
  assert.match(questions[0]?.context ?? "", /interrupt buyers/);
  assert.match(questions[0]?.context ?? "", /legacy order controller/);
  assert.deepEqual(questions[0]?.options, [
    "Preserve sessions until expiry — smoother migration",
    "Force restart — simpler implementation",
  ]);
  assert.equal(
    questions[0]?.unblocker,
    "Choose whether in-flight sessions survive deployment.",
  );
});

test("legacy bold question labels retain their context", () => {
  const questions = parseQuestions(`## Q-002

Authenticated buyer and subscription owner

**Why this matters:** The payer and owner may be different people.

**Options:**
- Buyer owns subscription
- Existing account owner remains authoritative

**Smallest unblocker:** Select the authoritative owner.
`);

  assert.equal(
    questions[0]?.question,
    "Authenticated buyer and subscription owner",
  );
  assert.match(questions[0]?.context ?? "", /different people/);
  assert.deepEqual(questions[0]?.options, [
    "Buyer owns subscription",
    "Existing account owner remains authoritative",
  ]);
  assert.equal(questions[0]?.unblocker, "Select the authoritative owner.");
});
