import type {
  Config,
  RoleConfig,
} from "../../../domains/configuration/types/config.js";
import type { ConfigurationRepository } from "../../../domains/configuration/interfaces/configuration-repository.js";
import type { CredentialStore } from "../../../domains/credentials/interfaces/credential-store.js";
import type { FeatureProvider } from "../../../domains/features/interfaces/feature-provider.js";
import type { Feature } from "../../../domains/features/types/feature.js";
import type { PortraitRegistry } from "../../../domains/roles/interfaces/portrait-registry.js";
import type { SkillResolution } from "../../../domains/roles/types/skill.js";
import type { DraftRepository } from "../../../domains/refinement/interfaces/draft-repository.js";
import type { ArchitectEventRepository } from "../../../domains/refinement/interfaces/architect-event-repository.js";
import type { RefinementRevisionRepository } from "../../../domains/refinement/interfaces/revision-repository.js";
import type { ResearchReportRepository } from "../../../domains/refinement/interfaces/research-report-repository.js";
import type { ReviewResultRepository } from "../../../domains/runs/interfaces/review-result-repository.js";
import type { RunEventRepository } from "../../../domains/runs/interfaces/run-event-repository.js";
import type { RunRecoveryRepository } from "../../../domains/runs/interfaces/run-recovery-repository.js";
import type { RunProcessRegistry } from "../../../domains/runs/repositories/run-process-registry.js";
import type { Run, RunResult } from "../../../domains/runs/types/run.js";
import type { CommandBus } from "../../bus/command-bus.js";
import type { QueryBus } from "../../bus/query-bus.js";
import type { DatabaseLifecycle } from "../../storage/interfaces/database-lifecycle.js";

export interface Application {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  close(): Promise<void>;
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
  executeRun?: (params: {
    projectRoot: string;
    run: Run;
    runDir: string;
    dryRun?: boolean;
    signal?: AbortSignal;
    eventRepository?: RunEventRepository;
    processRegistry?: RunProcessRegistry;
  }) => Promise<RunResult[]>;
  latestRuns: (projectRoot: string, config: Config) => Promise<Run[]>;
  configurationRepository: ConfigurationRepository;
  credentialStore: CredentialStore;
  featureProvider: FeatureProvider;
  portraitRegistry: PortraitRegistry;
  projectRoot?: string;
  stateDirectory?: string;
  refinementPrompt?: (
    feature: Feature,
    story: string,
    additionalContext?: string,
    questionsPath?: string,
  ) => string;
  runArchitect?: (params: {
    projectRoot: string;
    prompt: string;
    logFile: string;
  }) => Promise<{ logFile: string }>;
  cancelArchitect?: (featureId: string) => boolean;
  builtinRoleRoot?: string;
  resolveRoleGuidance?: (params: {
    projectRoot: string;
    roleName: string;
    role: RoleConfig;
    builtinRoot: string;
  }) => Promise<SkillResolution>;
}

export interface BootstrapRepositories {
  readonly drafts?: DraftRepository;
  readonly architectEvents?: ArchitectEventRepository;
  readonly revisions?: RefinementRevisionRepository;
  readonly researchReports?: ResearchReportRepository;
  readonly runEvents: RunEventRepository;
  readonly reviews: ReviewResultRepository;
  readonly recovery?: RunRecoveryRepository;
}

export interface ApplicationBootstrapContext {
  readonly commandBus: CommandBus;
  readonly queryBus: QueryBus;
  readonly dependencies: BootstrapDependencies;
  readonly projectRoot?: string;
  readonly repositories: BootstrapRepositories;
  readonly processRegistry: RunProcessRegistry;
}

export interface ApplicationBootstrapService {
  register(context: ApplicationBootstrapContext): void;
}

export interface ApplicationBootstrapComposition {
  readonly context: ApplicationBootstrapContext;
  readonly lifecycle: DatabaseLifecycle;
}
