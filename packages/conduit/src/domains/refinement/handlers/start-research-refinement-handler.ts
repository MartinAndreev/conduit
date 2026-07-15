import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import type { Config } from "@domains/configuration/types/config.js";
import type { Feature } from "@domains/features/types/feature.js";
import type { Run, RunResult } from "@domains/runs/types/run.js";
import type {
  StartResearchRefinementCommand,
  StartResearchRefinementResult,
} from "@domains/refinement/interfaces/commands/start-research-refinement.js";
import type { CommandHandler } from "@system/bus/command-bus.js";
import type { RunEventRepository } from "@domains/runs/interfaces/run-event-repository.js";
import type { RunProcessRegistry } from "@domains/runs/repositories/run-process-registry.js";
import type { ResearchReportRepository } from "@domains/refinement/interfaces/research-report-repository.js";
import { redactSecrets } from "@system/storage/security/secret-redaction.js";
import { parseAgentResponseV1 } from "@domains/runs/validation/agent-response-validator.js";
import { validateAgentResponseForAssignment } from "@domains/runs/validation/agent-semantic-validator.js";
import { renderResearchReport } from "@domains/runs/validation/research-renderer.js";
import { agentResponseContractPrompt } from "@domains/runs/assets/agent-response-contract.js";
import type { RunRecoveryRepository } from "@domains/runs/interfaces/run-recovery-repository.js";

const activeResearch = new Map<string, AbortController>();

export function cancelResearchForFeature(featureId: string): boolean {
  const controller = activeResearch.get(featureId);
  if (!controller) return false;
  controller.abort();
  return true;
}

export interface StartResearchRefinementDependencies {
  readonly projectRoot: string;
  readonly builtinRoleRoot: string;
  readonly loadConfig: (projectRoot: string) => Promise<Config>;
  readonly findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  readonly planRun: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
    roleNames: string[];
    builtinRoot: string;
  }) => Promise<{ run: Run; runDir: string }>;
  readonly executeRun: (params: {
    projectRoot: string;
    run: Run;
    runDir: string;
    dryRun?: boolean;
    signal?: AbortSignal;
    eventRepository?: RunEventRepository;
    processRegistry?: RunProcessRegistry;
  }) => Promise<RunResult[]>;
  readonly eventRepository: RunEventRepository;
  readonly processRegistry: RunProcessRegistry;
  readonly reportRepository: ResearchReportRepository;
  readonly recoveryRepository: RunRecoveryRepository;
}

export function createStartResearchRefinementHandler(
  deps: StartResearchRefinementDependencies,
): CommandHandler<
  StartResearchRefinementCommand,
  StartResearchRefinementResult
> {
  return async (command) => {
    try {
      const config = await deps.loadConfig(deps.projectRoot);
      if (!config.roles.researcher)
        throw new Error("No researcher role is configured for refinement.");
      const feature = await deps.findFeature({
        projectRoot: deps.projectRoot,
        config,
        featureId: command.featureId,
      });
      const { run, runDir } = await deps.planRun({
        projectRoot: deps.projectRoot,
        config,
        featureId: feature.id,
        roleNames: ["researcher"],
        builtinRoot: deps.builtinRoleRoot,
      });
      const researcher = run.roles[0];
      if (!researcher) throw new Error("Could not prepare the researcher run.");
      // Research preflight is always read-only, even when an older project
      // configuration omitted the flag. It reads the project in place and
      // writes only its dedicated report artifact under the run directory.
      researcher.readOnly = true;
      researcher.prompt += `\n\n# Refinement research assignment (authoritative)\n\nInvestigate the repository context needed to refine this feature. Do not edit source code, specification files, contracts, or the feature packet.\n\n## Feature request\n\n${redactSecrets(command.story)}\n\nReturn only AgentResponseV1 JSON. Put relevant files, call paths, confirmed facts, constraints, existing tests, assumptions, and questions in findings, evidence, risks, and questions. Cite paths for every repository claim. Do not propose a prompt, implementation plan, or production-code patch.`;
      await writeFile(researcher.promptFile, researcher.prompt);
      const reportFile = `conduit://research/${encodeURIComponent(feature.id)}`;
      const runnerReportFile = path.join(runDir, "researcher-output.md");
      researcher.finalOutputFile = runnerReportFile;
      researcher.prompt += `

# Research report delivery (authoritative)

${agentResponseContractPrompt()}

Conduit renders the human-readable Markdown report from the validated JSON; do not attempt to write the report or any other file yourself. Do not include runner setup, prompts, tool calls, command transcripts, model metadata, progress messages, Markdown fences, or prose around the JSON. Do not modify source code, packet artifacts, or any repository files.`;
      await writeFile(researcher.promptFile, researcher.prompt);
      const controller = new AbortController();
      activeResearch.set(feature.id, controller);
      await deps.recoveryRepository.saveSnapshot(run);
      void deps
        .executeRun({
          projectRoot: deps.projectRoot,
          run,
          runDir,
          dryRun: false,
          signal: controller.signal,
          eventRepository: deps.eventRepository,
          processRegistry: deps.processRegistry,
        })
        .then(async ([result]) => {
          if (!result || result.status !== "completed") return;
          const capturedFile = await readFile(runnerReportFile, "utf8").catch(
            () => "",
          );
          const report = capturedFile.trim()
            ? capturedFile
            : (result.stdout ?? "");
          if (!report.trim()) {
            await deps.eventRepository.append({
              type: "error",
              runId: run.id,
              roleId: researcher.name,
              timestamp: new Date().toISOString(),
              payload: {
                kind: "error",
                code: "RESEARCH_REPORT_MISSING",
                message: "Researcher completed without returning a report.",
                recoverable: true,
              },
            });
            await deps.recoveryRepository.markInterrupted(
              run.id,
              "Researcher completed without returning a report.",
            );
            return;
          }
          const structural = parseAgentResponseV1(report);
          const semantic = structural.valid && structural.value
            ? validateAgentResponseForAssignment(structural.value, {
                roleKind: "research",
                ownedPaths: [],
              })
            : structural;
          if (!structural.valid || !semantic.valid || !structural.value) {
            await deps.eventRepository.append({
              type: "error",
              runId: run.id,
              roleId: researcher.name,
              timestamp: new Date().toISOString(),
              payload: {
                kind: "error",
                code: "RESEARCH_PROTOCOL_INVALID",
                message: `Researcher returned invalid AgentResponseV1: ${[...structural.issues, ...semantic.issues].map((item) => `${item.path}: ${item.message}`).join("; ")}`,
                recoverable: true,
              },
            });
            return;
          }
          const sanitizedReport = redactSecrets(renderResearchReport(structural.value));
          await writeFile(runnerReportFile, report);
          await deps.reportRepository.save(feature.id, sanitizedReport);
          await deps.recoveryRepository.saveSnapshot(run, 1);
        })
        .catch(async (error) => {
          if (controller.signal.aborted)
            await deps.recoveryRepository.markCancelled(run.id);
          else
            await deps.recoveryRepository.markInterrupted(
              run.id,
              error instanceof Error ? error.message : String(error),
            );
        })
        .finally(() => activeResearch.delete(feature.id));
      return { success: true, data: { runId: run.id, reportFile } };
    } catch (error) {
      return {
        success: false,
        error: {
          code: "START_RESEARCH_REFINEMENT_ERROR",
          message: error instanceof Error ? error.message : String(error),
          cause: error,
        },
      };
    }
  };
}
