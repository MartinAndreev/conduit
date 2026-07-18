import assert from "node:assert/strict";
import test from "node:test";
import { renderClarificationAnswers } from "../../src/tui/components/ArchitectClarifications.js";

const questions = [
  {
    id: "Q-001",
    question: "Choose a canvas size",
    options: ["480 × 640", "Other"],
  },
  {
    id: "Q-002",
    question: "Choose physics tuning",
    options: ["Beginner", "Standard"],
  },
] as const;

test("clarification selections are serialized per question", () => {
  assert.equal(
    renderClarificationAnswers(questions, [
      "480 × 640",
      "Use gravity 850 and a 160px gap.",
    ]),
    "## Q-001\n\n480 × 640\n\n## Q-002\n\nUse gravity 850 and a 160px gap.",
  );
});
