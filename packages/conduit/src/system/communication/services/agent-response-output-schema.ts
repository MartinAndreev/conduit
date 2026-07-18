const text = { type: "string" } as const;

function objectSchema<
  const Properties extends Readonly<Record<string, unknown>>,
  const Required extends readonly (keyof Properties & string)[],
>(properties: Properties, required: Required) {
  return {
    type: "object",
    additionalProperties: false,
    properties,
    required,
  } as const;
}

function arrayOf<const Items extends Readonly<Record<string, unknown>>>(
  items: Items,
) {
  return { type: "array", items } as const;
}

const artifact = objectSchema(
  {
    path: text,
    category: {
      type: "string",
      enum: [
        "source",
        "test",
        "spec",
        "contract",
        "documentation",
        "report",
        "configuration",
        "other",
      ],
    },
    purpose: text,
    action: {
      type: "string",
      enum: ["created", "modified", "deleted", "inspected"],
    },
  },
  ["path", "category", "purpose", "action"],
);

const finding = objectSchema(
  {
    severity: {
      type: "string",
      enum: ["info", "warning", "error", "critical"],
    },
    category: text,
    message: text,
    evidence: arrayOf(text),
  },
  ["severity", "category", "message", "evidence"],
);

const verification = objectSchema(
  {
    operation: text,
    outcome: {
      type: "string",
      enum: ["passed", "failed", "skipped", "blocked", "unknown"],
    },
    summary: text,
  },
  ["operation", "outcome", "summary"],
);

const decision = objectSchema({ decision: text, rationale: text }, [
  "decision",
  "rationale",
]);

const blocker = objectSchema(
  { blocker: text, impact: text, minimumUnblocker: text },
  ["blocker", "impact", "minimumUnblocker"],
);

const question = objectSchema(
  {
    question: text,
    whyItMatters: text,
    context: text,
    options: arrayOf(text),
    smallestUnblocker: text,
  },
  ["question", "whyItMatters", "context", "options", "smallestUnblocker"],
);

const risk = objectSchema(
  {
    risk: text,
    category: {
      type: "string",
      enum: [
        "technical",
        "integration",
        "security",
        "compatibility",
        "verification",
        "operational",
        "other",
      ],
    },
    mitigation: text,
  },
  ["risk", "category", "mitigation"],
);

const evidence = objectSchema(
  {
    kind: {
      type: "string",
      enum: [
        "path",
        "line",
        "symbol",
        "contract",
        "command",
        "url",
        "runner_event",
        "other",
      ],
    },
    reference: text,
  },
  ["kind", "reference"],
);

const memoryProposal = objectSchema(
  {
    scope: { type: "string", enum: ["project"] },
    content: text,
    rationale: text,
  },
  ["scope", "content", "rationale"],
);

const globalPromotionProposal = objectSchema(
  { content: text, rationale: text, evidence: arrayOf(text) },
  ["content", "rationale", "evidence"],
);

const verdict = {
  anyOf: [
    { type: "null" },
    objectSchema(
      {
        decision: {
          type: "string",
          enum: [
            "approved",
            "rejected",
            "passed",
            "failed",
            "needs_changes",
            "inconclusive",
          ],
        },
        rationale: text,
      },
      ["decision", "rationale"],
    ),
  ],
} as const;

export const agentResponseOutputSchema = objectSchema(
  {
    protocolVersion: { type: "string", const: "1.0" },
    status: {
      type: "string",
      enum: ["completed", "partial", "blocked", "needs_input", "failed"],
    },
    summary: text,
    verdict,
    artifacts: arrayOf(artifact),
    findings: arrayOf(finding),
    verification: arrayOf(verification),
    decisions: arrayOf(decision),
    blockers: arrayOf(blocker),
    questions: arrayOf(question),
    risks: arrayOf(risk),
    evidence: arrayOf(evidence),
    memoryProposals: arrayOf(memoryProposal),
    globalPromotionProposals: arrayOf(globalPromotionProposal),
  },
  [
    "protocolVersion",
    "status",
    "summary",
    "verdict",
    "artifacts",
    "findings",
    "verification",
    "decisions",
    "blockers",
    "questions",
    "risks",
    "evidence",
    "memoryProposals",
    "globalPromotionProposals",
  ],
);

const toolFinding = {
  ...finding,
  properties: {
    ...finding.properties,
    evidence: text,
    path: text,
    line: { type: "integer", minimum: 1, maximum: 1_000_000 },
    suggestedRemediation: text,
  },
} as const;

const toolVerification = {
  ...verification,
  properties: {
    ...verification.properties,
    exitCode: { type: "integer", minimum: -1, maximum: 255 },
    evidence: arrayOf(text),
  },
} as const;

const toolDecision = {
  ...decision,
  properties: { ...decision.properties, affectedPaths: arrayOf(text) },
} as const;

const toolEvidence = {
  ...evidence,
  properties: { ...evidence.properties, summary: text },
} as const;

const toolMemoryProposal = {
  ...memoryProposal,
  properties: { ...memoryProposal.properties, evidence: arrayOf(text) },
} as const;

const toolGlobalPromotionProposal = {
  ...globalPromotionProposal,
  properties: { ...globalPromotionProposal.properties, evidence: text },
} as const;

export const agentResponseToolInputSchema = {
  ...agentResponseOutputSchema,
  properties: {
    ...agentResponseOutputSchema.properties,
    verdict: {
      type: "string",
      enum: [
        "none",
        "approved",
        "rejected",
        "passed",
        "failed",
        "needs_changes",
        "inconclusive",
      ],
    },
    verdictRationale: text,
    findings: arrayOf(toolFinding),
    verification: arrayOf(toolVerification),
    decisions: arrayOf(toolDecision),
    evidence: arrayOf(toolEvidence),
    memoryProposals: arrayOf(toolMemoryProposal),
    globalPromotionProposals: arrayOf(toolGlobalPromotionProposal),
  },
  required: [...agentResponseOutputSchema.required, "verdictRationale"],
} as const;
