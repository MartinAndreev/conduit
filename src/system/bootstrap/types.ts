import type {
  Config,
  RoleConfig,
} from "../../domains/configuration/types/config.js";
import type { Feature } from "../../domains/features/types/feature.js";
import type { Run, RunResult } from "../../domains/runs/types/run.js";
import type { SkillResolution } from "../../domains/roles/types/skill.js";

export interface ApplicationDependencies {
  initializeProject: (
    projectRoot: string,
    templateRoot: string,
    embeddedTemplates?: Record<string, string>,
  ) => Promise<{ createdConfig: boolean; configFile: string }>;
  loadConfig: (projectRoot: string) => Promise<Config>;
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
  writeStory: (feature: Feature, story: string) => Promise<string>;
  writeTestCases: (feature: Feature, testCases: string) => Promise<string>;
  readStory: (feature: Feature) => Promise<string>;
  refinementPrompt: (feature: Feature, story: string) => string;
  collectRefinement: () => Promise<{ story: string; testCases: string }>;
  collectArchitectAnswers: (questions: string) => Promise<string>;
  runArchitect: (params: {
    projectRoot: string;
    prompt: string;
    logFile: string;
    onProgress?: (message: string) => void;
    onTranscript?: (transcript: string) => void;
  }) => Promise<{ logFile: string }>;
  startDashboard: (params: {
    projectRoot: string;
    config: Config;
    runs: Run[];
    selectedRunId: string;
  }) => Promise<void>;
  startArchitectRunView: (params: {
    featureId: string;
    onUserClose?: () => void;
  }) => Promise<{
    update: (transcript: string) => void;
    complete: () => void;
    waitForClose: () => Promise<{ user: boolean }>;
    close: (params?: { user?: boolean }) => void;
  }>;
  startWorkerRunView: (params: {
    featureId: string;
    roles: string[];
    onCancel?: () => void;
    onUserClose?: () => void;
  }) => Promise<{
    updateStatus: (status: string) => void;
    appendEvent: (event: string) => void;
    close: (params?: { user?: boolean }) => void;
  }>;
  planRun: (params: {
    projectRoot: string;
    config: Config;
    featureId: string;
    roleNames: string[];
    builtinRoot: string;
    fetchSkills?: boolean;
  }) => Promise<{ run: Run; runDir: string }>;
  executeRun: (params: {
    projectRoot: string;
    run: Run;
    runDir: string;
    dryRun?: boolean;
    onProgress?: (message: string) => void;
    onChange?: (params: { summary: string; preview: string }) => void;
    signal?: AbortSignal;
  }) => Promise<RunResult[]>;
  latestRuns: (projectRoot: string, config: Config) => Promise<Run[]>;
  resolveSkill: (params: {
    projectRoot: string;
    roleName: string;
    role: RoleConfig;
    builtinRoot: string;
    allowNetwork?: boolean;
  }) => Promise<SkillResolution>;
  builtinRoot: string;
  templatesRoot: string;
  roleTemplates: Record<string, string>;
  output: (message: string) => void;
  progress: <T>(
    text: string,
    work: (params?: { setText?: (text: string) => void }) => Promise<T>,
  ) => Promise<T>;
}
