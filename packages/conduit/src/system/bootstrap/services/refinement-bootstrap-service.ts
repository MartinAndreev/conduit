import { loadConfig } from "../../../domains/configuration/repositories/project-config.js";
import {
  findFeature,
  writeStory,
  writeTestCases,
} from "../../../domains/features/repositories/feature-packet-repository.js";
import { createApproveRefinementHandler } from "../../../domains/refinement/handlers/approve-refinement-handler.js";
import { createCancelArchitectRefinementHandler } from "../../../domains/refinement/handlers/cancel-architect-refinement-handler.js";
import { createCancelResearchRefinementHandler } from "../../../domains/refinement/handlers/cancel-research-refinement-handler.js";
import { createDiscardDraftHandler } from "../../../domains/refinement/handlers/discard-draft-handler.js";
import { createGetArchitectEventsHandler } from "../../../domains/refinement/handlers/get-architect-events-handler.js";
import { createGetDraftHandler } from "../../../domains/refinement/handlers/get-draft-handler.js";
import { createGetRefinementRevisionHandler } from "../../../domains/refinement/handlers/get-refinement-revision-handler.js";
import { createGetResearchReportHandler } from "../../../domains/refinement/handlers/get-research-report-handler.js";
import { createListDraftsHandler } from "../../../domains/refinement/handlers/list-drafts-handler.js";
import { createResumeDraftHandler } from "../../../domains/refinement/handlers/resume-draft-handler.js";
import { createReviewRefinementPacketHandler } from "../../../domains/refinement/handlers/review-refinement-packet-handler.js";
import { createSaveDraftHandler } from "../../../domains/refinement/handlers/save-draft-handler.js";
import { createStartArchitectRefinementHandler } from "../../../domains/refinement/handlers/start-architect-refinement-handler.js";
import {
  cancelResearchForFeature,
  createStartResearchRefinementHandler,
} from "../../../domains/refinement/handlers/start-research-refinement-handler.js";
import { createSubmitArchitectAnswersHandler } from "../../../domains/refinement/handlers/submit-architect-answers-handler.js";
import { resolveSkill } from "../../../domains/roles/repositories/skill-resolver.js";
import type { CommandHandler } from "../../bus/command-bus.js";
import type { QueryHandler } from "../../bus/query-bus.js";
import type {
  ApplicationBootstrapContext,
  ApplicationBootstrapService,
} from "../interfaces/application-bootstrap.js";

export class RefinementBootstrapService implements ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void {
    const { commandBus, queryBus, projectRoot, repositories } = context;

    if (repositories.drafts) {
      commandBus.register(
        "saveDraft",
        createSaveDraftHandler(repositories.drafts) as CommandHandler,
      );
      commandBus.register(
        "discardDraft",
        createDiscardDraftHandler(repositories.drafts) as CommandHandler,
      );
      commandBus.register(
        "resumeDraft",
        createResumeDraftHandler(repositories.drafts) as CommandHandler,
      );
      queryBus.register(
        "getDraft",
        createGetDraftHandler(repositories.drafts) as QueryHandler,
      );
      queryBus.register(
        "listDrafts",
        createListDraftsHandler(repositories.drafts) as QueryHandler,
      );
    }

    if (repositories.architectEvents)
      queryBus.register(
        "getArchitectEvents",
        createGetArchitectEventsHandler(
          repositories.architectEvents,
        ) as QueryHandler,
      );

    if (!projectRoot) return;
    commandBus.register(
      "approveRefinement",
      createApproveRefinementHandler({
        loadConfig,
        findFeature,
        writeStory,
        writeTestCases,
        projectRoot,
      }) as CommandHandler,
    );

    this.registerResearch(context);
    this.registerArchitect(context);
    this.registerRevisionLoop(context);
  }

  private registerResearch(context: ApplicationBootstrapContext): void {
    const {
      commandBus,
      queryBus,
      dependencies,
      projectRoot,
      repositories,
      processRegistry,
    } = context;
    if (
      !projectRoot ||
      !dependencies.executeRun ||
      !dependencies.builtinRoleRoot ||
      !repositories.researchReports ||
      !repositories.recovery
    )
      return;

    commandBus.register(
      "startResearchRefinement",
      createStartResearchRefinementHandler({
        projectRoot,
        builtinRoleRoot: dependencies.builtinRoleRoot,
        loadConfig: dependencies.loadConfig,
        findFeature: dependencies.findFeature,
        planRun: dependencies.planRun,
        executeRun: dependencies.executeRun,
        eventRepository: repositories.runEvents,
        processRegistry,
        reportRepository: repositories.researchReports,
        recoveryRepository: repositories.recovery,
      }) as CommandHandler,
    );
    queryBus.register(
      "getResearchReport",
      createGetResearchReportHandler({
        repository: repositories.researchReports,
      }) as QueryHandler,
    );
    commandBus.register(
      "cancelResearchRefinement",
      createCancelResearchRefinementHandler(
        cancelResearchForFeature,
      ) as CommandHandler,
    );
  }

  private registerArchitect(context: ApplicationBootstrapContext): void {
    const { commandBus, dependencies, projectRoot, repositories } = context;
    if (
      projectRoot &&
      dependencies.refinementPrompt &&
      dependencies.runArchitect &&
      repositories.revisions &&
      repositories.researchReports
    )
      commandBus.register(
        "startArchitectRefinement",
        createStartArchitectRefinementHandler({
          projectRoot,
          loadConfig: dependencies.loadConfig,
          findFeature: dependencies.findFeature,
          refinementPrompt: dependencies.refinementPrompt,
          projectRoleGuidance: async (config) => {
            const architect = config.roles.architect;
            if (!architect || !dependencies.builtinRoleRoot) return "";
            const resolve = dependencies.resolveRoleGuidance ?? resolveSkill;
            const guidance = await resolve({
              projectRoot,
              roleName: "architect",
              role: architect,
              builtinRoot: dependencies.builtinRoleRoot,
            });
            return guidance.content;
          },
          runArchitect: dependencies.runArchitect,
          revisionRepository: repositories.revisions,
          researchReportRepository: repositories.researchReports,
          clarificationQuestionRepository: repositories.clarificationQuestions,
        }) as CommandHandler,
      );

    if (dependencies.cancelArchitect)
      commandBus.register(
        "cancelArchitectRefinement",
        createCancelArchitectRefinementHandler(
          dependencies.cancelArchitect,
        ) as CommandHandler,
      );
  }

  private registerRevisionLoop(context: ApplicationBootstrapContext): void {
    const { commandBus, queryBus, dependencies, projectRoot, repositories } =
      context;
    if (!projectRoot || !repositories.revisions) return;
    const handlerDependencies = {
      projectRoot,
      loadConfig: dependencies.loadConfig,
      findFeature: dependencies.findFeature,
      repository: repositories.revisions,
      clarificationQuestionRepository: repositories.clarificationQuestions,
    };
    commandBus.register(
      "submitArchitectAnswers",
      createSubmitArchitectAnswersHandler(
        handlerDependencies,
      ) as CommandHandler,
    );
    commandBus.register(
      "reviewRefinementPacket",
      createReviewRefinementPacketHandler(
        handlerDependencies,
      ) as CommandHandler,
    );
    queryBus.register(
      "getRefinementRevision",
      createGetRefinementRevisionHandler(handlerDependencies) as QueryHandler,
    );
  }
}
