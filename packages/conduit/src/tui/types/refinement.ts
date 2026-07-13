import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RefinementDraft } from "@domains/refinement/types/draft.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
} from "@domains/refinement/types/revision.js";
import type { ArchitectPreferences } from "@domains/refinement/types/architect-preferences.js";

export type RefinementView =
  | "loading"
  | "form"
  | "packet"
  | "preview"
  | "research"
  | "researchReview"
  | "architect"
  | "clarifications"
  | "review"
  | "error";

export type ArchitectLifecycle = "idle" | "running" | "cancelled" | "failed";

export interface RefinementPacketContent {
  readonly spec: string;
  readonly plan: string;
  readonly tasks: string;
  readonly testCases: string;
}

export interface RefinementFormViewModel {
  readonly activeFieldIndex: number;
  readonly values: Record<string, string>;
  readonly cursorPosition: number;
  readonly setActiveValue: (value: string) => void;
  readonly submit: () => void;
  readonly tip: string;
}

export interface RefinementControllerState {
  readonly feature: FeatureReadModel | null;
  readonly draft: RefinementDraft | null;
  readonly view: RefinementView;
  readonly values: Record<string, string>;
  readonly loading: boolean;
  readonly error: string | null;
  readonly architectEnabled: boolean;
  readonly researchEnabled: boolean;
  readonly architectPreferences: ArchitectPreferences;
  readonly architectLifecycle: ArchitectLifecycle;
  readonly architectRunning: boolean;
  readonly packetContent: RefinementPacketContent | null;
  readonly revision: RefinementRevision | null;
  readonly questions: readonly ClarificationQuestion[];
  readonly researchReport: string | null;
  readonly researchRunId: string | null;
}

export interface RefinementControllerActions {
  setView(view: Exclude<RefinementView, "loading" | "error">): void;
  setValues(values: Record<string, string>): void;
  saveDraft(): Promise<void>;
  submitForm(values: Record<string, string>): void;
  approvePreview(): Promise<void>;
  rejectPreview(): void;
  quitPreview(): void;
  toggleArchitect(): void;
  toggleResearch(): void;
  cycleArchitectPreference(kind: "effort" | "detailLevel"): void;
  startResearch(): void;
  acceptResearch(): void;
  cancelResearch(): Promise<void>;
  editPacketBrief(): void;
  cancelArchitect(): Promise<void>;
  submitAnswers(answers: string): Promise<void>;
  approvePacket(): Promise<void>;
  requestPacketChanges(feedback: string): Promise<void>;
}

export interface RefinementLifecycleState {
  readonly view: RefinementView;
  readonly loading: boolean;
  readonly error: string | null;
  readonly architectLifecycle: ArchitectLifecycle;
  readonly previousView: Exclude<
    RefinementView,
    "loading" | "error" | "architect"
  >;
}

export type RefinementLifecycleAction =
  | { type: "view"; view: Exclude<RefinementView, "loading" | "error"> }
  | { type: "loaded"; view: Exclude<RefinementView, "loading" | "error"> }
  | { type: "startArchitect" }
  | { type: "architectComplete"; view: "clarifications" | "review" }
  | { type: "architectCancelled" }
  | { type: "error"; error: string };
