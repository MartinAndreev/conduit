import assert from "node:assert/strict";
import test from "node:test";
import { parseAgentResponseV1 } from "../src/domains/runs/validation/agent-response-validator.js";
import {
  collectOwnershipWarnings,
  validateAgentResponseForAssignment,
} from "../src/domains/runs/validation/agent-semantic-validator.js";
import { agentProcessEnvironment } from "../src/domains/runs/repositories/run-orchestrator.js";
import type { AgentResponseV1 } from "../src/domains/runs/types/agent-protocol.js";
import { createAgentAssignmentV1 } from "../src/domains/runs/factories/agent-assignment-factory.js";
import { validateAgentAssignmentV1 } from "../src/domains/runs/validation/agent-assignment-validator.js";
import { renderResearchReport } from "../src/domains/runs/validation/research-renderer.js";
import { renderClarificationQuestions } from "../src/domains/runs/validation/clarification-renderer.js";
import { roleKindForRole } from "../src/domains/runs/validation/agent-semantic-validator.js";
import { AgentRoleKind } from "../src/domains/roles/enums/agent-role-kind.js";
import { agentResponseContractPrompt } from "../src/domains/runs/assets/agent-response-contract.js";

const base: AgentResponseV1 = {
  protocolVersion: "1.0",
  status: "completed",
  summary: "Completed work.",
  verdict: null,
  artifacts: [],
  findings: [],
  verification: [],
  decisions: [],
  blockers: [],
  questions: [],
  risks: [],
  evidence: [],
  memoryProposals: [],
  globalPromotionProposals: [],
};

test("AgentResponseV1 rejects prose, unknown properties, invalid paths, and secrets", () => {
  assert.equal(parseAgentResponseV1("```json\n{}\n```").valid, false);
  assert.equal(
    parseAgentResponseV1(JSON.stringify({ ...base, extra: true })).valid,
    false,
  );
  assert.equal(
    parseAgentResponseV1(
      JSON.stringify({
        ...base,
        artifacts: [
          {
            path: "../x",
            category: "source",
            purpose: "x",
            action: "modified",
          },
        ],
      }),
    ).valid,
    false,
  );
  assert.equal(
    parseAgentResponseV1(
      JSON.stringify({ ...base, summary: "api_key=secret-value" }),
    ).valid,
    false,
  );
});

test("AgentResponseV1 rejects every specified structural failure mode", () => {
  assert.equal(parseAgentResponseV1('{"broken":').valid, false);
  assert.equal(
    parseAgentResponseV1(`${JSON.stringify(base)}\n${JSON.stringify(base)}`)
      .valid,
    false,
  );
  assert.equal(
    parseAgentResponseV1(
      JSON.stringify({ ...base, summary: "x".repeat(2_001) }),
    ).valid,
    false,
  );
  assert.equal(
    parseAgentResponseV1(JSON.stringify({ ...base, status: "unknown" })).valid,
    false,
  );
  assert.equal(
    parseAgentResponseV1(
      JSON.stringify({ ...base, summary: "x".repeat(256_001) }),
    ).valid,
    false,
  );
});

test("AgentAssignmentV1 is strict and normalized", () => {
  const assignment = createAgentAssignmentV1({
    assignmentId: "run:backend",
    role: "backend",
    roleKind: "implementation",
    objective: "Implement the approved task.",
    ownedPaths: ["src"],
    contextReferences: ["specs/007/spec.md"],
    acceptanceCriteria: ["Pass the approved cases."],
    contracts: ["specs/007/contracts/README.md"],
  });
  assert.equal(validateAgentAssignmentV1(assignment).valid, true);
  assert.equal(
    validateAgentAssignmentV1({ ...assignment, unknown: true }).valid,
    false,
  );
  assert.equal(
    validateAgentAssignmentV1({ ...assignment, ownedPaths: ["../src"] }).valid,
    false,
  );
  assert.deepEqual(
    createAgentAssignmentV1({
      ...assignment,
      ownedPaths: ["./", "./src/"],
    }).ownedPaths,
    [".", "src"],
  );
  for (const ownedPath of ["", "/"] as const) {
    assert.equal(
      validateAgentAssignmentV1(
        createAgentAssignmentV1({ ...assignment, ownedPaths: [ownedPath] }),
      ).valid,
      false,
    );
  }
});

test("configured custom role kinds override the built-in compatibility map", () => {
  assert.equal(roleKindForRole("mobile"), "custom");
  assert.equal(roleKindForRole("mobile", "implementation"), "implementation");
});

test("semantic policy differs by assignment role", () => {
  const parsed = parseAgentResponseV1(
    JSON.stringify({
      ...base,
      findings: [
        {
          severity: "info",
          category: "fact",
          message: "Repo uses CQRS",
          evidence: ["packages/conduit/src/system/bus"],
        },
      ],
    }),
  );
  assert.equal(parsed.valid, true);
  assert.equal(
    validateAgentResponseForAssignment(parsed.value!, {
      roleKind: "research",
      ownedPaths: [],
    }).valid,
    true,
  );
  assert.equal(
    validateAgentResponseForAssignment(parsed.value!, {
      roleKind: "reviewer",
      ownedPaths: [],
    }).valid,
    false,
  );
});

test("implementation completion requires artifacts and verification independent of ownership", () => {
  const response = parseAgentResponseV1(
    JSON.stringify({
      ...base,
      artifacts: [
        {
          path: "src/a.ts",
          category: "source",
          purpose: "change",
          action: "modified",
        },
      ],
      verification: [
        { operation: "bun test", outcome: "passed", summary: "ok" },
      ],
    }),
  ).value!;
  assert.equal(
    validateAgentResponseForAssignment(response, {
      roleKind: "implementation",
      ownedPaths: ["src"],
    }).valid,
    true,
  );
  assert.equal(
    validateAgentResponseForAssignment(response, {
      roleKind: "implementation",
      ownedPaths: ["docs"],
    }).valid,
    true,
  );
  assert.equal(
    collectOwnershipWarnings(response, {
      roleKind: "implementation",
      ownedPaths: ["docs"],
    }).length,
    1,
  );
});

test("authoritative observed changes must match claims and preserve ownership warnings", () => {
  const response: AgentResponseV1 = {
    ...base,
    artifacts: [
      {
        path: "src/a.ts",
        category: "source",
        purpose: "change",
        action: "modified",
      },
    ],
    verification: [
      { operation: "pnpm test", outcome: "passed", summary: "ok" },
    ],
  };
  assert.equal(
    validateAgentResponseForAssignment(response, {
      roleKind: "implementation",
      ownedPaths: ["src"],
      observedChangedFiles: ["src/a.ts"],
    }).valid,
    true,
  );
  const unexpectedResponse: AgentResponseV1 = {
    ...response,
    artifacts: [
      {
        path: "docs/a.md",
        category: "documentation",
        purpose: "integration documentation",
        action: "modified",
      },
    ],
  };
  const unexpectedPolicy = {
    roleKind: AgentRoleKind.Implementation,
    ownedPaths: ["src"],
    observedChangedFiles: ["docs/a.md"],
  } as const;
  assert.equal(
    validateAgentResponseForAssignment(unexpectedResponse, unexpectedPolicy)
      .valid,
    true,
  );
  assert.deepEqual(
    collectOwnershipWarnings(unexpectedResponse, unexpectedPolicy).map(
      (item) => item.message,
    ),
    [
      "reported modification outside assigned ownership: docs/a.md",
      "Conduit observed a change outside assigned ownership: docs/a.md",
    ],
  );
  assert.equal(
    validateAgentResponseForAssignment(response, {
      roleKind: "implementation",
      ownedPaths: ["src"],
      observedChangedFiles: ["docs/a.md"],
    }).valid,
    false,
  );
});

test("repository root ownership accepts root and nested changes", () => {
  const response: AgentResponseV1 = {
    ...base,
    artifacts: [
      {
        path: "package.json",
        category: "configuration",
        purpose: "configure the application",
        action: "modified",
      },
      {
        path: "src/main.ts",
        category: "source",
        purpose: "implement the application",
        action: "created",
      },
    ],
    verification: [
      { operation: "pnpm test", outcome: "passed", summary: "passed" },
    ],
  };

  assert.equal(
    validateAgentResponseForAssignment(response, {
      roleKind: AgentRoleKind.Implementation,
      ownedPaths: ["./"],
      observedChangedFiles: ["package.json", "src/main.ts"],
    }).valid,
    true,
  );
});

test("empty ownership produces warnings without granting forbidden or read-only access", () => {
  const response: AgentResponseV1 = {
    ...base,
    artifacts: [
      {
        path: "src/mobile.ts",
        category: "source",
        purpose: "mobile implementation",
        action: "modified",
      },
    ],
    verification: [
      { operation: "pnpm test", outcome: "passed", summary: "passed" },
    ],
  };
  const policy = {
    roleKind: AgentRoleKind.Implementation,
    ownedPaths: [],
    observedChangedFiles: ["src/mobile.ts"],
  } as const;
  const result = validateAgentResponseForAssignment(response, policy);

  assert.equal(result.valid, true);
  assert.equal(collectOwnershipWarnings(response, policy).length, 2);
  assert.equal(
    validateAgentResponseForAssignment(response, {
      ...policy,
      forbiddenPaths: ["src"],
    }).valid,
    false,
  );
  assert.equal(
    validateAgentResponseForAssignment(response, {
      ...policy,
      readOnly: true,
    }).valid,
    false,
  );
});

test("completed implementation responses reject failed or skipped verification", () => {
  for (const outcome of ["failed", "skipped", "blocked", "unknown"] as const) {
    const response: AgentResponseV1 = {
      ...base,
      artifacts: [
        {
          path: "src/a.ts",
          category: "source",
          purpose: "change",
          action: "modified",
        },
      ],
      verification: [
        { operation: "pnpm test", outcome, summary: "not successful" },
      ],
    };
    assert.equal(
      validateAgentResponseForAssignment(response, {
        roleKind: "implementation",
        ownedPaths: ["src"],
      }).valid,
      false,
    );
  }
});

test("completed research, QA, and review responses preserve negative verification evidence", () => {
  const verification = [
    {
      operation: "pnpm test",
      outcome: "failed" as const,
      exitCode: 1,
      summary: "The requested behavior is not implemented.",
      evidence: ["test output"],
    },
  ];
  const research: AgentResponseV1 = { ...base, verification };
  assert.equal(
    validateAgentResponseForAssignment(research, {
      roleKind: AgentRoleKind.Research,
      ownedPaths: [],
    }).valid,
    true,
  );

  const qualityAssurance: AgentResponseV1 = { ...base, verification };
  assert.equal(
    validateAgentResponseForAssignment(qualityAssurance, {
      roleKind: AgentRoleKind.QualityAssurance,
      ownedPaths: [],
    }).valid,
    true,
  );

  const reviewer: AgentResponseV1 = {
    ...base,
    verdict: {
      decision: "rejected",
      rationale: "The failing verification demonstrates a material defect.",
    },
    verification,
  };
  assert.equal(
    validateAgentResponseForAssignment(reviewer, {
      roleKind: AgentRoleKind.Reviewer,
      ownedPaths: [],
    }).valid,
    true,
  );
});

test("completed evaluative responses still reject skipped, blocked, or unknown verification", () => {
  for (const outcome of ["skipped", "blocked", "unknown"] as const) {
    const response: AgentResponseV1 = {
      ...base,
      verification: [
        { operation: "pnpm test", outcome, summary: "Not completed." },
      ],
    };
    assert.equal(
      validateAgentResponseForAssignment(response, {
        roleKind: AgentRoleKind.Research,
        ownedPaths: [],
      }).valid,
      false,
    );
  }
});

test("AgentResponseV1 prompt explains role-aware negative verification semantics", () => {
  const prompt = agentResponseContractPrompt();
  assert.match(
    prompt,
    /implementation assignments, completed status requires every reported/i,
  );
  assert.match(prompt, /Research, QA, and reviewer assignments/i);
  assert.match(prompt, /failed\s+product check is a finding/i);
});

test("rejected reviewer requires findings or a material rationale", () => {
  const rejected: AgentResponseV1 = {
    ...base,
    verdict: { decision: "rejected", rationale: "no" },
  };
  assert.equal(
    validateAgentResponseForAssignment(rejected, {
      roleKind: "reviewer",
      ownedPaths: [],
    }).valid,
    false,
  );
  assert.equal(
    validateAgentResponseForAssignment(
      {
        ...rejected,
        findings: [
          {
            severity: "error",
            category: "correctness",
            message: "A material defect remains.",
            evidence: ["src/a.ts:1"],
          },
        ],
      },
      { roleKind: "reviewer", ownedPaths: [] },
    ).valid,
    true,
  );
});

test("blocked and needs_input require structured sections", () => {
  assert.equal(
    validateAgentResponseForAssignment(
      { ...base, status: "blocked" },
      { roleKind: "custom", ownedPaths: [] },
    ).valid,
    false,
  );
  assert.equal(
    validateAgentResponseForAssignment(
      { ...base, status: "needs_input" },
      { roleKind: "custom", ownedPaths: [] },
    ).valid,
    false,
  );
});

test("architect clarification completes semantically without a run-state artifact claim", () => {
  const response: AgentResponseV1 = {
    ...base,
    status: "needs_input",
    questions: [
      {
        question: "Which progression model should the game use?",
        whyItMatters: "The choice changes persistence and level transitions.",
        context: "The approved story does not select a model.",
        options: ["Linear unlocks", "Independent levels"],
        smallestUnblocker: "Choose one progression model.",
      },
    ],
  };
  const result = validateAgentResponseForAssignment(response, {
    roleKind: AgentRoleKind.Architect,
    ownedPaths: ["specs"],
  });
  assert.equal(result.valid, true);
  assert.deepEqual(response.artifacts, []);
});

test("agent process environment removes database configuration", () => {
  const env = agentProcessEnvironment({
    TURSO_DATABASE_URL: "x",
    LIBSQL_AUTH_TOKEN: "y",
    CONDUIT_DB_PATH: "z",
    SAFE: "1",
  });
  assert.deepEqual(env, { SAFE: "1" });
});

test("human research and clarification Markdown is rendered from validated responses", () => {
  const research: AgentResponseV1 = {
    ...base,
    findings: [
      {
        severity: "info",
        category: "fact",
        message: "The bus owns dispatch.",
        evidence: ["src/system/bus"],
      },
    ],
  };
  assert.match(renderResearchReport(research), /# Research context/);
  const clarification: AgentResponseV1 = {
    ...base,
    status: "needs_input",
    questions: [
      {
        question: "Choose storage?",
        whyItMatters: "Changes durability.",
        context: "Two stores exist.",
        options: ["Local", "Remote"],
        smallestUnblocker: "Choose one.",
      },
    ],
  };
  assert.match(
    renderClarificationQuestions(clarification),
    /# Architect clarification questions/,
  );
});
