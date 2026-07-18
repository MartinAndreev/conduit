import { TursoDraftRepository } from "../../../domains/refinement/repositories/turso-draft-repository.js";
import { TursoArchitectEventRepository } from "../../../domains/refinement/repositories/turso-architect-event-repository.js";
import { TursoRefinementRevisionRepository } from "../../../domains/refinement/repositories/turso-revision-repository.js";
import { TursoResearchReportRepository } from "../../../domains/refinement/repositories/turso-research-report-repository.js";
import { TursoClarificationQuestionRepository } from "../../../domains/refinement/repositories/turso-clarification-question-repository.js";
import { TursoRunEventRepository } from "../../../domains/runs/repositories/turso-run-event-repository.js";
import { TursoReviewResultRepository } from "../../../domains/runs/repositories/turso-review-result-repository.js";
import { TursoRunRecoveryRepository } from "../../../domains/runs/repositories/turso-run-recovery-repository.js";
import { TursoConduitResultRecordRepository } from "../../../domains/runs/repositories/turso-conduit-result-record-repository.js";
import { TursoRuntimeEventRepository } from "../../../domains/runs/repositories/turso-runtime-event-repository.js";
import { TursoHarnessRuntimeStateRepository } from "../../../domains/runs/repositories/turso-harness-runtime-state-repository.js";
import { TursoRoleWorkspaceRepository } from "../../../domains/runs/repositories/turso-role-workspace-repository.js";
import { TursoSourceVersionRepository } from "../../../domains/source/repositories/turso-source-version-repository.js";
import { InMemoryRunEventRepository } from "../../../domains/runs/repositories/in-memory-run-event-repository.js";
import { InMemoryReviewResultRepository } from "../../../domains/runs/repositories/in-memory-review-result-repository.js";
import { createRunProcessRegistry } from "../../../domains/runs/repositories/run-process-registry.js";
import type { CommandBus } from "../../bus/command-bus.js";
import type { QueryBus } from "../../bus/query-bus.js";
import { ProjectDatabaseFactory } from "../../storage/factories/database-factories.js";
import { resolveProjectDatabasePaths } from "../../storage/factories/path-resolution.js";
import { DefaultDatabaseLifecycle } from "../../storage/repositories/database-lifecycle.js";
import { LazyDatabaseConnection } from "../../storage/repositories/lazy-database-connection.js";
import type {
  ApplicationBootstrapComposition,
  BootstrapDependencies,
} from "../interfaces/application-bootstrap.js";

export function createBootstrapComposition(
  commandBus: CommandBus,
  queryBus: QueryBus,
  dependencies: BootstrapDependencies,
): ApplicationBootstrapComposition {
  const projectRoot = dependencies.projectRoot;
  // One application-scoped connection is shared by every project repository.
  // It opens lazily after startup migration and is closed by the app lifecycle.
  const connection = projectRoot
    ? new LazyDatabaseConnection(
        new ProjectDatabaseFactory(
          projectRoot,
          undefined,
          dependencies.stateDirectory,
        ),
        resolveProjectDatabasePaths(projectRoot, dependencies.stateDirectory)
          .databasePath,
      )
    : undefined;
  const lifecycle = new DefaultDatabaseLifecycle();
  if (connection) lifecycle.registerConnection(connection);

  return {
    lifecycle,
    context: {
      commandBus,
      queryBus,
      dependencies,
      projectRoot,
      processRegistry: createRunProcessRegistry(),
      repositories: {
        drafts: connection ? new TursoDraftRepository(connection) : undefined,
        architectEvents: connection
          ? new TursoArchitectEventRepository(connection)
          : undefined,
        revisions: connection
          ? new TursoRefinementRevisionRepository(connection)
          : undefined,
        researchReports: connection
          ? new TursoResearchReportRepository(connection)
          : undefined,
        clarificationQuestions: connection
          ? new TursoClarificationQuestionRepository(connection)
          : undefined,
        runEvents: connection
          ? new TursoRunEventRepository(connection)
          : new InMemoryRunEventRepository(),
        reviews: connection
          ? new TursoReviewResultRepository(connection)
          : new InMemoryReviewResultRepository(),
        recovery: connection
          ? new TursoRunRecoveryRepository(connection)
          : undefined,
        resultRecords: connection
          ? new TursoConduitResultRecordRepository(connection)
          : undefined,
        runtimeEvents: connection
          ? new TursoRuntimeEventRepository(connection)
          : undefined,
        harnessRuntimeState: connection
          ? new TursoHarnessRuntimeStateRepository(connection)
          : undefined,
        roleWorkspaces: connection
          ? new TursoRoleWorkspaceRepository(connection)
          : undefined,
        sourceVersions: connection
          ? new TursoSourceVersionRepository(connection)
          : undefined,
      },
    },
  };
}
