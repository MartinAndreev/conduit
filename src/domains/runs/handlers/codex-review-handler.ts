import type { CommandHandler } from "../../../system/bus/command-bus.js";
import type {
  CodexReviewCommand,
  CodexReviewResult,
} from "../interfaces/commands/codex-review.js";
import type { ReviewResultRepository } from "../interfaces/review-result-repository.js";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../../features/types/feature.js";
import type { Run } from "../types/run.js";
import type { ReviewDecision, ReviewFinding } from "../types/review.js";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";

export interface CodexReviewDependencies {
  loadConfig: (projectRoot: string) => Promise<Config>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
}

function buildReviewPrompt(
  featureId: string,
  run: Run,
  diffs: Map<string, string>,
  packetContent: string,
): string {
  const diffSections = [...diffs.entries()]
    .map(
      ([role, diff]) =>
        `## Role: ${role}\n\`\`\`diff\n${diff.slice(0, 4000)}\n\`\`\``,
    )
    .join("\n\n");

  return `You are a code reviewer. Review the following implementation for feature ${featureId}.

## Approved Specification
${packetContent.slice(0, 4000)}

## Run Details
- Run ID: ${run.id}
- Feature: ${run.featureId}
- Roles: ${run.roles.map((r) => `${r.name} (${r.runner})`).join(", ")}
- Status: ${run.status}

## Authoritative Worktree Diffs
${diffSections}

## Review Instructions
1. Check if the implementation matches the approved specification
2. Look for bugs, security issues, and missing error handling
3. Verify tests are present and passing
4. Check for unresolved integration risks

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
  "followUp": "Optional follow-up instructions or null"
}`;
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

export function createCodexReviewHandler(
  deps: CodexReviewDependencies,
  reviewRepository: ReviewResultRepository,
): CommandHandler<CodexReviewCommand, CodexReviewResult> {
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
      const packetFile = path.join(feature.directory, "spec.md");
      let packetContent = "";
      try {
        packetContent = await readFile(packetFile, "utf8");
      } catch {
        packetContent = "No specification found";
      }

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

      // Invoke Codex review runner
      const promptFile = path.join(
        command.projectRoot,
        config.stateDir,
        "runs",
        command.runId,
        "review-prompt.md",
      );
      await (await import("node:fs/promises")).writeFile(promptFile, prompt);

      const codexResult = spawnSync(
        "codex",
        ["exec", `Read ${promptFile} and perform the review.`],
        {
          encoding: "utf8",
          timeout: 120_000,
        },
      );

      const output = codexResult.stdout ?? "";
      const { decision, findings, followUp } = parseReviewOutput(output);

      // Persist review result
      const reviewId = `codex-review-${command.runId}-${Date.now()}`;
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
