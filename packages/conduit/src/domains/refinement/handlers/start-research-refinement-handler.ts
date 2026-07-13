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
      researcher.prompt += `\n\n# Refinement research assignment (authoritative)\n\nInvestigate the repository context needed to refine this feature. Do not edit source code, specification files, contracts, or the feature packet.\n\n## Feature request\n\n${command.story}\n\nReturn a concise Markdown report with: relevant files and call paths; confirmed facts; constraints and risks; existing tests and conventions; assumptions that require validation; and product or technical questions that should inform the architect. Cite paths for every repository claim. Do not propose a prompt, implementation plan, or production-code patch.`;
      await writeFile(researcher.promptFile, researcher.prompt);
      await writeFile(
        path.join(runDir, "run.json"),
        JSON.stringify(run, null, 2) + "\n",
      );
      const reportFile = path.join(feature.directory, "research.md");
      const runnerReportFile = path.join(runDir, "researcher-output.md");
      researcher.prompt += `

# Research report delivery (authoritative)

Write the final Markdown report to \`${runnerReportFile}\`. This is the only file you may create or modify. Do not include runner setup, prompts, tool calls, command transcripts, model metadata, or progress messages in that file. Do not modify source code, packet artifacts, or any other repository files.`;
      await writeFile(researcher.promptFile, researcher.prompt);
      const controller = new AbortController();
      activeResearch.set(feature.id, controller);
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
          const report = await readFile(runnerReportFile, "utf8").catch(
            () => "",
          );
          if (!report.trim()) {
            await deps.eventRepository.append({
              type: "error",
              runId: run.id,
              roleId: researcher.name,
              timestamp: new Date().toISOString(),
              payload: {
                kind: "error",
                code: "RESEARCH_REPORT_MISSING",
                message:
                  "Researcher completed without writing its report artifact.",
                recoverable: true,
              },
            });
            return;
          }
          await writeFile(reportFile, `# Research context\n\n${report}\n`);
        })
        .catch(() => {})
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
