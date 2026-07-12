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
  }) => Promise<RunResult[]>;
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
      const controller = new AbortController();
      activeResearch.set(feature.id, controller);
      const [result] = await deps
        .executeRun({
          projectRoot: deps.projectRoot,
          run,
          runDir,
          dryRun: false,
          signal: controller.signal,
        })
        .finally(() => activeResearch.delete(feature.id));
      if (!result || result.status !== "completed")
        throw new Error(result?.error ?? "Researcher did not complete.");
      const report = result.output?.trim() ?? "";
      if (!report) throw new Error("Researcher completed without a report.");
      const reportFile = path.join(feature.directory, "research.md");
      await writeFile(reportFile, `# Research context\n\n${report}\n`);
      const saved = await readFile(reportFile, "utf8");
      return { success: true, data: { report: saved, reportFile } };
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
