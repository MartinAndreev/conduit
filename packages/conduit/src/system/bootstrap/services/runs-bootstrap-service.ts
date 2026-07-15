import { createCancelRunHandler } from "../../../domains/runs/handlers/cancel-run-handler.js";
import { createFinalReviewHandler } from "../../../domains/runs/handlers/final-review-handler.js";
import { createGetReviewResultHandler } from "../../../domains/runs/handlers/get-review-result-handler.js";
import { createGetRunDiffHandler } from "../../../domains/runs/handlers/get-run-diff-handler.js";
import { createGetRunEventsHandler } from "../../../domains/runs/handlers/get-run-events-handler.js";
import { createGetRunHandler } from "../../../domains/runs/handlers/get-run-handler.js";
import { createReviewRunHandler } from "../../../domains/runs/handlers/review-run-handler.js";
import type { StartFeatureRunCommand } from "../../../domains/runs/interfaces/commands/start-feature-run.js";
import { WorktreeDiffReader } from "../../../domains/runs/repositories/worktree-diff-reader.js";
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

    if (projectRoot && executeRun) {
      commandBus.register("startFeatureRun", (async (
        command: StartFeatureRunCommand,
      ) => {
        const config = await dependencies.loadConfig(projectRoot);
        const roleNames = [...command.roleNames];
        if (!roleNames.length)
          return {
            success: false,
            error: {
              code: "NO_RUN_ROLES",
              message: "Configure at least one role before starting a run.",
            },
          };
        const { run, runDir } = await dependencies.planRun({
          projectRoot,
          config,
          featureId: command.featureId,
          roleNames,
          builtinRoot: dependencies.builtinRoleRoot ?? "",
        });
        const initialSnapshot = await repositories.recovery?.saveSnapshot(run);
        let snapshotVersion = initialSnapshot?.version;
        let snapshotWrite = Promise.resolve();
        const persistSnapshot = (): Promise<void> => {
          if (!repositories.recovery) return Promise.resolve();
          snapshotWrite = snapshotWrite.then(async () => {
            const persisted = await repositories.recovery?.saveSnapshot(
              run,
              snapshotVersion,
            );
            snapshotVersion = persisted?.version;
          });
          return snapshotWrite;
        };
        void executeRun({
          projectRoot,
          run,
          runDir,
          dryRun: false,
          eventRepository: repositories.runEvents,
          processRegistry,
          onRoleWorkspaceReady: persistSnapshot,
        })
          .then(async () => {
            await persistSnapshot();
            if (run.status === "cancelled")
              await repositories.recovery?.markCancelled(run.id);
          })
          .catch(async (error) => {
            await repositories.recovery?.markInterrupted(
              run.id,
              error instanceof Error ? error.message : String(error),
            );
          });
        return { success: true, data: { runId: run.id } };
      }) as CommandHandler);
    }

    queryBus.register("latestRuns", (async (
      query: Query & { projectRoot: string },
    ) => {
      if (repositories.recovery) {
        const snapshots = await repositories.recovery.listSnapshots();
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
      commandBus.register(
        "finalReview",
        createFinalReviewHandler(
          {
            loadConfig: dependencies.loadConfig,
            findFeature: dependencies.findFeature,
          },
          repositories.reviews,
          repositories.recovery,
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
