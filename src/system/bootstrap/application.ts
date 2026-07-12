import type { Config } from "../../domains/configuration/types/config.js";
import type { ConfigurationRepository } from "../../domains/configuration/repositories/configuration-repository.js";
import type { CredentialStore } from "../../domains/credentials/interfaces/credential-store.js";
import type { FeatureProvider } from "../../domains/features/interfaces/feature-provider.js";
import type { Feature } from "../../domains/features/types/feature.js";
import type { PortraitRegistry } from "../../domains/roles/interfaces/portrait-registry.js";
import type { Run } from "../../domains/runs/types/run.js";
import { CommandBus } from "../bus/command-bus.js";
import type { Command, CommandHandler } from "../bus/command-bus.js";
import { QueryBus } from "../bus/query-bus.js";
import type { Query, QueryHandler } from "../bus/query-bus.js";
import { createResolveSettingsHandler } from "../../domains/configuration/handlers/resolve-settings-handler.js";
import { createGetCredentialHandler } from "../../domains/credentials/handlers/get-credential-handler.js";
import { createListCredentialKeysHandler } from "../../domains/credentials/handlers/list-credential-keys-handler.js";
import { createSetCredentialHandler } from "../../domains/credentials/handlers/set-credential-handler.js";
import { createDeleteCredentialHandler } from "../../domains/credentials/handlers/delete-credential-handler.js";
import { createListFeaturesHandler } from "../../domains/features/handlers/list-features-handler.js";
import { createGetFeatureHandler } from "../../domains/features/handlers/get-feature-handler.js";
import { createGetFeatureContentHandler } from "../../domains/features/handlers/get-feature-content-handler.js";
import { createUpdateFeatureMetadataHandler } from "../../domains/features/handlers/update-feature-metadata-handler.js";
import { createCreateFeatureHandler } from "../../domains/features/handlers/create-feature-handler.js";
import { createListPortraitsHandler } from "../../domains/roles/handlers/list-portraits-handler.js";
import { createSaveDraftHandler } from "../../domains/refinement/handlers/save-draft-handler.js";
import { createDiscardDraftHandler } from "../../domains/refinement/handlers/discard-draft-handler.js";
import { createResumeDraftHandler } from "../../domains/refinement/handlers/resume-draft-handler.js";
import { createGetDraftHandler } from "../../domains/refinement/handlers/get-draft-handler.js";
import { createListDraftsHandler } from "../../domains/refinement/handlers/list-drafts-handler.js";
import { createApproveRefinementHandler } from "../../domains/refinement/handlers/approve-refinement-handler.js";
import { createGetArchitectEventsHandler } from "../../domains/refinement/handlers/get-architect-events-handler.js";
import { createStartArchitectRefinementHandler } from "../../domains/refinement/handlers/start-architect-refinement-handler.js";
import { createCancelArchitectRefinementHandler } from "../../domains/refinement/handlers/cancel-architect-refinement-handler.js";
import { createSubmitArchitectAnswersHandler } from "../../domains/refinement/handlers/submit-architect-answers-handler.js";
import { createReviewRefinementPacketHandler } from "../../domains/refinement/handlers/review-refinement-packet-handler.js";
import { createGetRefinementRevisionHandler } from "../../domains/refinement/handlers/get-refinement-revision-handler.js";
import { FileDraftRepository } from "../../domains/refinement/repositories/file-draft-repository.js";
import { FileArchitectEventRepository } from "../../domains/refinement/repositories/file-architect-event-repository.js";
import { FileRefinementRevisionRepository } from "../../domains/refinement/repositories/file-revision-repository.js";
import {
  findFeature,
  writeStory,
  writeTestCases,
} from "../../domains/features/repositories/feature-packet-repository.js";
import { loadConfig } from "../../domains/configuration/repositories/project-config.js";

export interface Application {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
}

export interface BootstrapDependencies {
  loadConfig: (projectRoot: string) => Promise<Config>;
  initializeProject: (
    projectRoot: string,
    templateRoot: string,
    embeddedTemplates?: Record<string, string>,
  ) => Promise<{ createdConfig: boolean; configFile: string }>;
  createFeature: (params: {
    projectRoot: string;
    config: Config;
    title: string;
  }) => Promise<Feature>;
  findFeature: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
  }) => Promise<Feature>;
  planRun: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
    roleNames: string[];
    builtinRoot: string;
    fetchSkills?: boolean;
  }) => Promise<{ run: Run; runDir: string }>;
  latestRuns: (projectRoot: string, config: Config) => Promise<Run[]>;
  configurationRepository: ConfigurationRepository;
  credentialStore: CredentialStore;
  featureProvider: FeatureProvider;
  portraitRegistry: PortraitRegistry;
  projectRoot?: string;
  refinementPrompt?: (feature: Feature, story: string) => string;
  runArchitect?: (params: {
    projectRoot: string;
    prompt: string;
    logFile: string;
  }) => Promise<{ logFile: string }>;
  cancelArchitect?: (featureId: string) => boolean;
}

interface InitProjectPayload {
  projectRoot: string;
  templateRoot: string;
}

interface ProjectBootstrapState {
  initialized: boolean;
  configPath?: string;
}

export function createApplication(deps: BootstrapDependencies): Application {
  const commandBus = new CommandBus();
  const queryBus = new QueryBus();

  const projectRoot = deps.projectRoot;
  const draftRepository = projectRoot
    ? new FileDraftRepository(projectRoot)
    : null;
  const architectEventRepository = projectRoot
    ? new FileArchitectEventRepository(projectRoot)
    : null;
  const revisionRepository = projectRoot
    ? new FileRefinementRevisionRepository()
    : null;

  commandBus.register("initializeProject", (async (
    cmd: Command & InitProjectPayload,
  ) => {
    const result = await deps.initializeProject(
      cmd.projectRoot,
      cmd.templateRoot,
    );
    return { success: true, data: result };
  }) as CommandHandler);

  commandBus.register(
    "setCredential",
    createSetCredentialHandler(deps.credentialStore) as CommandHandler,
  );

  commandBus.register(
    "deleteCredential",
    createDeleteCredentialHandler(deps.credentialStore) as CommandHandler,
  );

  commandBus.register(
    "updateFeatureMetadata",
    createUpdateFeatureMetadataHandler(deps.featureProvider) as CommandHandler,
  );
  if (projectRoot)
    commandBus.register(
      "createFeature",
      createCreateFeatureHandler({
        projectRoot,
        loadConfig: deps.loadConfig,
        createFeature: deps.createFeature,
      }) as CommandHandler,
    );

  queryBus.register("projectBootstrapState", (async (
    q: Query & { projectRoot: string },
  ) => {
    try {
      await deps.loadConfig(q.projectRoot);
      return {
        success: true,
        data: {
          initialized: true,
          configPath: `${q.projectRoot}/conduit.yml`,
        },
      };
    } catch {
      return {
        success: true,
        data: { initialized: false } satisfies ProjectBootstrapState,
      };
    }
  }) as QueryHandler);

  queryBus.register("latestRuns", (async (
    q: Query & { projectRoot: string },
  ) => {
    const config = await deps.loadConfig(q.projectRoot);
    const runs = await deps.latestRuns(q.projectRoot, config);
    return { success: true, data: runs };
  }) as QueryHandler);

  queryBus.register(
    "resolveSettings",
    createResolveSettingsHandler(deps.configurationRepository) as QueryHandler,
  );

  queryBus.register(
    "getCredential",
    createGetCredentialHandler(deps.credentialStore) as QueryHandler,
  );

  queryBus.register(
    "listCredentialKeys",
    createListCredentialKeysHandler(deps.credentialStore) as QueryHandler,
  );

  queryBus.register(
    "listFeatures",
    createListFeaturesHandler(deps.featureProvider) as QueryHandler,
  );

  queryBus.register(
    "getFeature",
    createGetFeatureHandler(deps.featureProvider) as QueryHandler,
  );
  queryBus.register(
    "getFeatureContent",
    createGetFeatureContentHandler(deps.featureProvider) as QueryHandler,
  );

  queryBus.register(
    "listPortraits",
    createListPortraitsHandler(deps.portraitRegistry) as QueryHandler,
  );

  if (draftRepository) {
    commandBus.register(
      "saveDraft",
      createSaveDraftHandler(draftRepository) as CommandHandler,
    );

    commandBus.register(
      "discardDraft",
      createDiscardDraftHandler(draftRepository) as CommandHandler,
    );

    commandBus.register(
      "resumeDraft",
      createResumeDraftHandler(draftRepository) as CommandHandler,
    );

    queryBus.register(
      "getDraft",
      createGetDraftHandler(draftRepository) as QueryHandler,
    );

    queryBus.register(
      "listDrafts",
      createListDraftsHandler(draftRepository) as QueryHandler,
    );
  }

  if (projectRoot) {
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
    if (deps.refinementPrompt && deps.runArchitect) {
      commandBus.register(
        "startArchitectRefinement",
        createStartArchitectRefinementHandler({
          projectRoot,
          loadConfig: deps.loadConfig,
          findFeature: deps.findFeature,
          refinementPrompt: deps.refinementPrompt,
          runArchitect: deps.runArchitect,
          revisionRepository: revisionRepository!,
        }) as CommandHandler,
      );
    }
    if (deps.cancelArchitect)
      commandBus.register(
        "cancelArchitectRefinement",
        createCancelArchitectRefinementHandler(
          deps.cancelArchitect,
        ) as CommandHandler,
      );
    if (revisionRepository) {
      commandBus.register(
        "submitArchitectAnswers",
        createSubmitArchitectAnswersHandler({
          projectRoot,
          loadConfig: deps.loadConfig,
          findFeature: deps.findFeature,
          repository: revisionRepository,
        }) as CommandHandler,
      );
      commandBus.register(
        "reviewRefinementPacket",
        createReviewRefinementPacketHandler({
          projectRoot,
          loadConfig: deps.loadConfig,
          findFeature: deps.findFeature,
          repository: revisionRepository,
        }) as CommandHandler,
      );
      queryBus.register(
        "getRefinementRevision",
        createGetRefinementRevisionHandler({
          projectRoot,
          loadConfig: deps.loadConfig,
          findFeature: deps.findFeature,
          repository: revisionRepository,
        }) as QueryHandler,
      );
    }
  }

  if (architectEventRepository) {
    queryBus.register(
      "getArchitectEvents",
      createGetArchitectEventsHandler(architectEventRepository) as QueryHandler,
    );
  }

  return { commandBus, queryBus };
}
