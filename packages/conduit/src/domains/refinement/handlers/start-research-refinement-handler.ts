import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
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
import { RunnerEventProvenance } from "@domains/runs/enums/runner-event-provenance.js";
import { validateAgentAssignmentV1 } from "@domains/runs/validation/agent-assignment-validator.js";

const activeResearch = new Map<string, AbortController>();

async function createIsolatedResearchWorkspace(
  projectRoot: string,
  run: Run,
): Promise<{ parent: string; workspace: string }> {
  const parent = await mkdtemp(
    path.join(tmpdir(), "conduit-research-workspace-"),
  );
  const workspace = path.join(parent, "workspace");
  const excluded = [
    path.join(projectRoot, ".git"),
    path.join(projectRoot, ".conduit"),
    run.stateDirectory,
    run.worktreeRoot,
  ]
    .filter((entry): entry is string => Boolean(entry))
    .map((entry) => path.resolve(entry));
  try {
    await cp(projectRoot, workspace, {
      recursive: true,
      filter: (source) => {
        const resolved = path.resolve(source);
        return !excluded.some(
          (entry) =>
            resolved === entry || resolved.startsWith(`${entry}${path.sep}`),
        );
      },
    });
    return { parent, workspace };
  } catch (cause) {
    await rm(parent, { recursive: true, force: true });
    throw cause;
  }
}

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
    sharedReadOnlyWorkspace?: boolean;
  }) => Promise<{ run: Run; runDir: string }>;
  readonly executeRun: (params: {
    projectRoot: string;
    run: Run;
    runDir: string;
    dryRun?: boolean;
    onRoleWorkspaceReady?: () => Promise<void>;
    signal?: AbortSignal;
    eventRepository?: RunEventRepository;
    processRegistry?: RunProcessRegistry;
    roleWorkspaceRepository?: import("../../runs/interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
    sharedReadOnlyWorkspace?: boolean;
  }) => Promise<RunResult[]>;
  readonly eventRepository: RunEventRepository;
  readonly processRegistry: RunProcessRegistry;
  readonly reportRepository: ResearchReportRepository;
  readonly recoveryRepository: RunRecoveryRepository;
  readonly roleWorkspaceRepository?: import("../../runs/interfaces/role-workspace-repository.js").RoleWorkspaceRepository;
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
        sharedReadOnlyWorkspace: true,
      });
      const researcher = run.roles[0];
      if (!researcher) throw new Error("Could not prepare the researcher run.");
      // Research preflight is always read-only, even when an older project
      // configuration omitted the flag. Agents inspect an isolated snapshot so
      // a fallback harness cannot mutate the user's dirty or unborn checkout.
      researcher.readOnly = true;
      if (!researcher.assignment)
        throw new Error("Researcher run is missing AgentAssignmentV1.");
      researcher.assignment = {
        ...researcher.assignment,
        roleKind: "research",
        ownedPaths: [],
        expectedCapabilities: ["repository-read"],
        objective:
          "Investigate the feature request using repository evidence. Do not edit repository files. Return relevant files, call paths, confirmed facts, constraints, existing tests, assumptions, risks, and questions in AgentResponseV1.",
        acceptanceCriteria: [
          "Cite bounded evidence for every repository claim.",
          "Do not propose an implementation patch or modify repository files.",
          "Separate confirmed findings, risks, and questions in the response.",
        ],
      };
      const assignmentValidation = validateAgentAssignmentV1(
        researcher.assignment,
      );
      if (!assignmentValidation.valid)
        throw new Error(
          `Invalid research assignment: ${assignmentValidation.issues.map((item) => `${item.path}: ${item.message}`).join("; ")}`,
        );
      researcher.context = `${researcher.context ?? ""}\n\n# Refinement research request\n\n${redactSecrets(command.story)}\n\n${agentResponseContractPrompt()}\n`;
      researcher.workspaceAssignmentHash = createHash("sha256")
        .update(JSON.stringify(researcher.assignment), "utf8")
        .digest("hex");
      researcher.prompt = `${JSON.stringify(researcher.assignment, null, 2)}\n`;
      await writeFile(researcher.contextFile!, researcher.context);
      await writeFile(researcher.promptFile, researcher.prompt);
      const reportFile = `conduit://research/${encodeURIComponent(feature.id)}`;
      const responseFile = path.join(runDir, "researcher-agent-response.json");
      researcher.finalOutputFile = responseFile;
      const controller = new AbortController();
      const isolatedWorkspace = await createIsolatedResearchWorkspace(
        deps.projectRoot,
        run,
      );
      researcher.worktree = isolatedWorkspace.workspace;
      activeResearch.set(feature.id, controller);
      try {
        await deps.recoveryRepository.saveSnapshot(run);
      } catch (cause) {
        activeResearch.delete(feature.id);
        await rm(isolatedWorkspace.parent, { recursive: true, force: true });
        throw cause;
      }
      void deps
        .executeRun({
          projectRoot: deps.projectRoot,
          run,
          runDir,
          dryRun: false,
          signal: controller.signal,
          eventRepository: deps.eventRepository,
          processRegistry: deps.processRegistry,
          roleWorkspaceRepository: deps.roleWorkspaceRepository,
          sharedReadOnlyWorkspace: true,
        })
        .then(async ([result]) => {
          await deps.recoveryRepository.saveSnapshot(run, 1);
          if (!result || result.status !== "completed") return;
          const capturedFile = await readFile(responseFile, "utf8").catch(
            () => "",
          );
          const report = capturedFile.trim()
            ? capturedFile
            : (result.stdout ?? "");
          if (!report.trim()) {
            await deps.eventRepository.append({
              type: "error",
              provenance: RunnerEventProvenance.ConduitObserved,
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
          const semantic =
            structural.valid && structural.value
              ? validateAgentResponseForAssignment(structural.value, {
                  roleKind: "research",
                  ownedPaths: [],
                })
              : structural;
          if (!structural.valid || !semantic.valid || !structural.value) {
            await deps.eventRepository.append({
              type: "error",
              provenance: RunnerEventProvenance.ConduitObserved,
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
          const sanitizedReport = redactSecrets(
            renderResearchReport(structural.value),
          );
          await deps.reportRepository.save(feature.id, sanitizedReport);
        })
        .catch(async (error) => {
          run.status = controller.signal.aborted ? "cancelled" : "failed";
          for (const role of run.roles)
            if (role.status !== "completed")
              role.status = controller.signal.aborted ? "cancelled" : "failed";
          await deps.recoveryRepository
            .saveSnapshot(run, 1)
            .catch(() => undefined);
          if (controller.signal.aborted)
            await deps.recoveryRepository.markCancelled(run.id);
          else
            await deps.recoveryRepository.markInterrupted(
              run.id,
              error instanceof Error ? error.message : String(error),
            );
        })
        .finally(async () => {
          activeResearch.delete(feature.id);
          await rm(isolatedWorkspace.parent, { recursive: true, force: true });
        });
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
