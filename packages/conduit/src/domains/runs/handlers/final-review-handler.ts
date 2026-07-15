import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  FinalReviewCommand,
  FinalReviewResult,
} from "../interfaces/commands/final-review.js";
import type { ReviewResultRepository } from "../interfaces/review-result-repository.js";
import type { RunRecoveryRepository } from "../interfaces/run-recovery-repository.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../../features/types/feature.js";
import type { Run } from "../types/run.js";
import type { ReviewDecision, ReviewFinding } from "../types/review.js";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { coreRoleContract } from "../../roles/assets/core-role-contract.js";
import {
  agentProcessEnvironment,
  commandForRole,
} from "../repositories/run-orchestrator.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import { agentResponseContractPrompt } from "../assets/agent-response-contract.js";
import { parseAgentResponseV1 } from "../validation/agent-response-validator.js";
import { validateAgentResponseForAssignment } from "../validation/agent-semantic-validator.js";
import {
  captureFinalResponse,
  configureFinalOutputCapture,
} from "@system/runners/registry.js";
import { createAgentAssignmentV1 } from "../factories/agent-assignment-factory.js";
import { validateAgentAssignmentV1 } from "../validation/agent-assignment-validator.js";

export interface FinalReviewDependencies {
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
}

export function buildReviewPrompt(
  featureId: string,
  run: Run,
  diffs: Map<string, string>,
  packetContent: string,
): string {
  const diffSections = [...diffs.entries()]
    .map(([role, diff]) => `## Role: ${role}\n\`\`\`diff\n${diff}\n\`\`\``)
    .join("\n\n");

  return `# Final review contract

${coreRoleContract("reviewer")}

Review the following implementation for feature ${featureId}.

## Approved Packet
${packetContent}

## Run Details
- Run ID: ${run.id}
- Feature: ${run.featureId}
- Roles: ${run.roles.map((r) => `${r.name} (${r.runner})`).join(", ")}
- Status: ${run.status}

## Authoritative Worktree Diffs
${diffSections}

## Review Instructions
1. Read every supplied diff section and compare it against every applicable acceptance criterion and contract. Do not approve based on summaries or stated intent.
2. Inspect changed logic and its callers, consumers, and established repository conventions in context. Apply the complete final review contract above.
3. Treat test evidence strictly: do not claim tests pass unless supplied evidence proves it. Missing relevant coverage or unavailable verification is an unresolved concern, not a pass.
4. Each material finding must name the affected file and line when available, explain the observable risk, and state the smallest concrete remediation.
5. Reject for any material correctness, security, reliability, performance, maintainability, compatibility, operability, or verification concern. Approve only when every material requirement is independently verified.
6. Do not add speculative findings. Findings must be grounded in the supplied packet, diff, or directly implied behavior.

${agentResponseContractPrompt()}

Reviewer-specific requirements: set verdict.decision to approved or rejected. Put review findings in findings with path and line when applicable. Do not include a separate review_result format.`;
}

async function readApprovedPacket(directory: string): Promise<string> {
  const packetFiles = ["spec.md", "plan.md", "tasks.md", "test-cases.md"];
  const contractsDirectory = path.join(directory, "contracts");
  const contractFiles = await readdir(contractsDirectory, {
    withFileTypes: true,
  }).catch(() => []);
  const files = [
    ...packetFiles.map((name) => path.join(directory, name)),
    ...contractFiles
      .filter((entry) => entry.isFile())
      .map((entry) => path.join(contractsDirectory, entry.name))
      .sort(),
  ];
  const sections = await Promise.all(
    files.map(async (file) => {
      const content = await readFile(file, "utf8").catch(() => "");
      return content.trim()
        ? `### ${path.relative(directory, file)}\n\n${content.trim()}`
        : "";
    }),
  );
  return sections.filter(Boolean).join("\n\n");
}

function parseReviewOutput(output: string): {
  decision: ReviewDecision;
  findings: ReviewFinding[];
  followUp: string | undefined;
} {
  const structural = parseAgentResponseV1(output);
  if (!structural.valid || !structural.value) {
    return {
      decision: "rejected",
      findings: [
        {
          severity: "warning",
          message: `Reviewer did not return valid AgentResponseV1: ${structural.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
        },
      ],
      followUp:
        "Reviewer must return exactly one valid AgentResponseV1 JSON object.",
    };
  }
  const semantic = validateAgentResponseForAssignment(structural.value, {
    roleKind: "reviewer",
    ownedPaths: [],
  });
  const findings: ReviewFinding[] = structural.value.findings.map(
    (finding) => ({
      severity: finding.severity === "critical" ? "error" : finding.severity,
      file: finding.path,
      line: finding.line,
      message: finding.message,
    }),
  );
  if (!semantic.valid) {
    findings.push({
      severity: "warning",
      message: `Reviewer response failed semantic policy: ${semantic.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
    });
  }
  const approved =
    semantic.valid &&
    structural.value.status === "completed" &&
    structural.value.verdict?.decision === "approved";
  return {
    decision: approved ? "approved" : "rejected",
    findings,
    followUp: approved
      ? undefined
      : (structural.value.verdict?.rationale ?? "Manual review required"),
  };
}

export function createFinalReviewHandler(
  deps: FinalReviewDependencies,
  reviewRepository: ReviewResultRepository,
  recoveryRepository: RunRecoveryRepository,
): CommandHandler<FinalReviewCommand, FinalReviewResult> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(command.projectRoot);

      const snapshot = await recoveryRepository.loadSnapshot(command.runId);
      if (!snapshot) {
        return {
          success: false,
          error: {
            code: "RUN_NOT_FOUND",
            message: `Run ${command.runId} not found`,
          },
        };
      }
      const run: Run = snapshot.run;

      // Load feature packet
      const feature = await deps.findFeature({
        projectRoot: command.projectRoot,
        config: await deps.loadConfig(command.projectRoot),
        featureId: command.featureId,
      });
      const packetContent =
        (await readApprovedPacket(feature.directory)) ||
        "No approved packet content found";

      // Gather authoritative worktree diffs
      const diffs = new Map<string, string>();
      for (const role of run.roles) {
        if (role.worktree) {
          const diffResult = spawnSync(
            "git",
            [
              "-C",
              role.worktree,
              "diff",
              "--no-ext-diff",
              "--unified=3",
              "HEAD",
            ],
            { encoding: "utf8" },
          );
          if (diffResult.status === 0 && diffResult.stdout.trim()) {
            diffs.set(role.name, diffResult.stdout.trim());
          }
        }
      }

      // Build review prompt
      const prompt = buildReviewPrompt(
        command.featureId,
        run,
        diffs,
        packetContent,
      );

      // Invoke the configured reviewer runner. Review policy stays provider-neutral.
      const contextFile = path.join(
        command.projectRoot,
        config.stateDir,
        "runs",
        command.runId,
        "review-context.md",
      );
      const promptFile = path.join(
        path.dirname(contextFile),
        "review-assignment.json",
      );
      const outputFile = path.join(
        path.dirname(contextFile),
        "review-agent-response.json",
      );
      await (
        await import("node:fs/promises")
      ).writeFile(contextFile, redactSecrets(prompt));

      const reviewer = config.roles.reviewer;
      if (!reviewer)
        throw new Error("No reviewer role is configured for final review.");
      const assignment = createAgentAssignmentV1({
        assignmentId: `${command.runId}:final-review`,
        role: "reviewer",
        roleKind: "reviewer",
        objective: `Independently review feature ${command.featureId} and return an approved or rejected AgentResponseV1 verdict grounded in the referenced packet and diffs.`,
        ownedPaths: [],
        contextReferences: [path.relative(command.projectRoot, contextFile)],
        acceptanceCriteria: [
          "Trace every approved acceptance criterion to authoritative evidence.",
          "Reject when any material concern or required evidence remains.",
        ],
        contracts: [`specs/${command.featureId}-*/contracts/README.md`],
        expectedCapabilities: ["repository-read"],
      });
      const assignmentValidation = validateAgentAssignmentV1(assignment);
      if (!assignmentValidation.valid)
        throw new Error(
          `Invalid final-review assignment: ${assignmentValidation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
        );
      await (
        await import("node:fs/promises")
      ).writeFile(promptFile, `${JSON.stringify(assignment, null, 2)}\n`);
      const [reviewCommand, reviewArgs] = commandForRole(reviewer, promptFile);
      const capturedArgs = configureFinalOutputCapture(
        reviewer.runner,
        reviewArgs,
        outputFile,
      );
      const reviewResult = spawnSync(reviewCommand, capturedArgs, {
        cwd: command.projectRoot,
        encoding: "utf8",
        timeout: 120_000,
        env: agentProcessEnvironment(),
      });

      const capturedOutput = await readFile(outputFile, "utf8").catch(() => "");
      if (capturedOutput)
        await (
          await import("node:fs/promises")
        ).writeFile(outputFile, redactSecrets(capturedOutput));
      const output = captureFinalResponse(
        reviewer.runner,
        command.runId,
        "reviewer",
        reviewResult.stdout ?? "",
        reviewResult.stderr ?? "",
        capturedOutput,
      );
      const { decision, findings, followUp } = parseReviewOutput(output);

      // Persist review result
      const reviewId = `final-review-${command.runId}-${Date.now()}`;
      const evidencePaths = findings.filter((f) => f.file).map((f) => f.file!);

      await reviewRepository.save({
        reviewId,
        runId: command.runId,
        featureId: command.featureId,
        decision,
        findings,
        evidencePaths,
        followUp,
        reviewedAt: new Date().toISOString(),
      });

      return {
        success: true,
        data: {
          reviewId,
          decision,
          findingsCount: findings.length,
          evidencePaths,
          followUp,
        },
      };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "RUNNER_REVIEW_ERROR",
          message: `Failed to run the configured reviewer: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
