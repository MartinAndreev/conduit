import { createCancelRunHandler } from "../../../domains/runs/handlers/cancel-run-handler.js";
import { createFinalReviewHandler } from "../../../domains/runs/handlers/final-review-handler.js";
import { createGetReviewResultHandler } from "../../../domains/runs/handlers/get-review-result-handler.js";
import { createGetRunDiffHandler } from "../../../domains/runs/handlers/get-run-diff-handler.js";
import { createGetRunEventsHandler } from "../../../domains/runs/handlers/get-run-events-handler.js";
import { createGetRunHandler } from "../../../domains/runs/handlers/get-run-handler.js";
import { createGetWorkspaceContinuityHandler } from "../../../domains/runs/handlers/get-workspace-continuity-handler.js";
import { createGetRunResumeEligibilityHandler } from "../../../domains/runs/handlers/get-run-resume-eligibility-handler.js";
import { createReviewRunHandler } from "../../../domains/runs/handlers/review-run-handler.js";
import { createResumeRunHandler } from "../../../domains/runs/handlers/resume-run-handler.js";
import { createStartFeatureRunHandler } from "../../../domains/runs/handlers/start-feature-run-handler.js";
import { WorktreeDiffReader } from "../../../domains/runs/repositories/worktree-diff-reader.js";
import type { WorkspaceContinuity } from "../../../domains/runs/types/workspace-continuity.js";
import type { CommandHandler } from "../../bus/command-bus.js";
import type { Query, QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

export class RunsBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const {
      commandBus,
      queryBus,
      dependencies,
      projectRoot,
      repositories,
      processRegistry,
    } = context;
    const executeRun = dependencies.executeRun;

    if (
      projectRoot &&
      executeRun &&
      repositories.recovery &&
      repositories.roleWorkspaces
    ) {
      const resumeRunHandler = createResumeRunHandler(repositories.recovery, {
        projectRoot,
        executeRun,
        eventRepository: repositories.runEvents,
        resultRepository: repositories.resultRecords,
        runtimeEventRepository: repositories.runtimeEvents,
        processRegistry,
        roleWorkspaceRepository: repositories.roleWorkspaces,
      });
      commandBus.register("resumeRun", resumeRunHandler as CommandHandler);
      commandBus.register(
        "startFeatureRun",
        createStartFeatureRunHandler({
          projectRoot,
          builtinRoot: dependencies.builtinRoleRoot ?? "",
          loadConfig: dependencies.loadConfig,
          planRun: dependencies.planRun,
          executeRun,
          recoveryRepository: repositories.recovery,
          roleWorkspaceRepository: repositories.roleWorkspaces,
          eventRepository: repositories.runEvents,
          resultRepository: repositories.resultRecords,
          runtimeEventRepository: repositories.runtimeEvents,
          processRegistry,
          getContinuity: async (featureId, roleNames) => {
            const result = await queryBus.execute({
              type: "getWorkspaceContinuity",
              featureId,
              roleNames,
            });
            if (!result.success) throw new Error(result.error.message);
            return result.data as WorkspaceContinuity;
          },
          resumeRun: async (runId) =>
            resumeRunHandler({ type: "resumeRun", runId }),
        }) as CommandHandler,
      );
    }

    queryBus.register("latestRuns", (async (
      query: Query & { projectRoot: string },
    ) => {
      if (repositories.recovery) {
        const snapshots = await repositories.recovery.listSnapshots(20);
        return { success: true, data: snapshots.map(({ run }) => run) };
      }
      const config = await dependencies.loadConfig(query.projectRoot);
      const runs = await dependencies.latestRuns(query.projectRoot, config);
      return { success: true, data: runs };
    }) as QueryHandler);

    queryBus.register(
      "getRunEvents",
      createGetRunEventsHandler(repositories.runEvents) as QueryHandler,
    );
    if (repositories.recovery) {
      queryBus.register(
        "getRunDiff",
        createGetRunDiffHandler(
          new WorktreeDiffReader(),
          repositories.recovery,
        ) as QueryHandler,
      );
      queryBus.register(
        "getRun",
        createGetRunHandler(repositories.recovery) as QueryHandler,
      );
      if (repositories.roleWorkspaces && projectRoot)
        queryBus.register(
          "getWorkspaceContinuity",
          createGetWorkspaceContinuityHandler(
            projectRoot,
            repositories.recovery,
            repositories.roleWorkspaces,
            repositories.resultRecords,
          ) as QueryHandler,
        );
      queryBus.register(
        "getRunResumeEligibility",
        createGetRunResumeEligibilityHandler(
          repositories.recovery,
          repositories.resultRecords,
          repositories.roleWorkspaces,
        ) as QueryHandler,
      );
      commandBus.register(
        "finalReview",
        createFinalReviewHandler(
          {
            loadConfig: dependencies.loadConfig,
            findFeature: dependencies.findFeature,
          },
          repositories.reviews,
          repositories.recovery,
          repositories.resultRecords,
        ) as CommandHandler,
      );
    }

    commandBus.register(
      "reviewRun",
      createReviewRunHandler(repositories.reviews) as CommandHandler,
    );
    commandBus.register(
      "cancelRun",
      createCancelRunHandler(
        repositories.runEvents,
        processRegistry,
        repositories.recovery,
      ) as CommandHandler,
    );
    queryBus.register(
      "getReviewResult",
      createGetReviewResultHandler(repositories.reviews) as QueryHandler,
    );
  }
}
