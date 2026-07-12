import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  FinalReviewCommand,
  FinalReviewResult,
} from "../interfaces/commands/final-review.js";
import type { ReviewResultRepository } from "../interfaces/review-result-repository.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../../features/types/feature.js";
import type { Run } from "../types/run.js";
import type { ReviewDecision, ReviewFinding } from "../types/review.js";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { coreRoleContract } from "../../roles/assets/core-role-contract.js";
import { commandForRole } from "../repositories/run-orchestrator.js";

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

Respond in this exact JSON format:
{
  "decision": "approved" or "rejected",
  "findings": [
    {
      "severity": "info" | "warning" | "error",
      "file": "path/to/file.ts",
      "line": 42,
      "message": "Description of the finding"
    }
  ],
  "followUp": "Concrete verification or remediation required before approval, or null"
}`;
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
  try {
    // Try to extract JSON from the output
    const jsonMatch = output.match(/\{[\s\S]*"decision"[\s\S]*\}/);
    if (jsonMatch) {
      const parsed = JSON.parse(jsonMatch[0]) as {
        decision: string;
        findings?: Array<{
          severity: string;
          file?: string;
          line?: number;
          message: string;
        }>;
        followUp?: string | null;
      };
      const decision: ReviewDecision =
        parsed.decision === "approved" ? "approved" : "rejected";
      const findings: ReviewFinding[] = (parsed.findings ?? []).map((f) => ({
        severity:
          f.severity === "error"
            ? "error"
            : f.severity === "warning"
              ? "warning"
              : "info",
        file: f.file,
        line: f.line,
        message: f.message,
      }));
      return { decision, findings, followUp: parsed.followUp ?? undefined };
    }
  } catch {
    // Fall through to default
  }
  // Default to rejected if parsing fails
  return {
    decision: "rejected",
    findings: [
      { severity: "warning", message: "Could not parse review output" },
    ],
    followUp: "Manual review required",
  };
}

export function createFinalReviewHandler(
  deps: FinalReviewDependencies,
  reviewRepository: ReviewResultRepository,
): CommandHandler<FinalReviewCommand, FinalReviewResult> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(command.projectRoot);

      // Load run data
      const runFile = path.join(
        command.projectRoot,
        config.stateDir,
        "runs",
        command.runId,
        "run.json",
      );
      let run: Run;
      try {
        const raw = await readFile(runFile, "utf8");
        run = JSON.parse(raw);
      } catch {
        return {
          success: false,
          error: {
            code: "RUN_NOT_FOUND",
            message: `Run ${command.runId} not found`,
          },
        };
      }

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
      const promptFile = path.join(
        command.projectRoot,
        config.stateDir,
        "runs",
        command.runId,
        "review-prompt.md",
      );
      await (await import("node:fs/promises")).writeFile(promptFile, prompt);

      const reviewer = config.roles.reviewer;
      if (!reviewer)
        throw new Error("No reviewer role is configured for final review.");
      const [reviewCommand, reviewArgs] = commandForRole(reviewer, promptFile);
      const reviewResult = spawnSync(reviewCommand, reviewArgs, {
        cwd: command.projectRoot,
        encoding: "utf8",
        timeout: 120_000,
      });

      const output = reviewResult.stdout ?? "";
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
          code: "CODEX_REVIEW_ERROR",
          message: `Failed to run Codex review: ${error instanceof Error ? error.message : String(error)}`,
          cause: error,
        },
      };
    }
  };
}
