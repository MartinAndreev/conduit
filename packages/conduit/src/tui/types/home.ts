import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RolePortrait } from "@domains/roles/interfaces/role-portrait.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";

export type HomeInteraction =
  | { readonly kind: "idle" }
  | { readonly kind: "search"; readonly query: string }
  | { readonly kind: "create"; readonly title: string }
  | { readonly kind: "featureActions"; readonly actionIndex: number }
  | { readonly kind: "updateConfirmation"; readonly actionIndex: 0 | 1 };

export type HomeInteractionAction =
  | { readonly type: "search" }
  | { readonly type: "create" }
  | { readonly type: "actions" }
  | { readonly type: "idle" }
  | { readonly type: "append"; readonly value: string }
  | { readonly type: "setTitle"; readonly value: string }
  | { readonly type: "backspace" }
  | { readonly type: "nextAction" }
  | { readonly type: "previousAction" }
  | { readonly type: "openUpdateConfirmation" }
  | { readonly type: "selectUpdateAction"; readonly value: 0 | 1 };

export type HomeUpdateKeyDecision =
  | { readonly kind: "interaction"; readonly action: HomeInteractionAction }
  | { readonly kind: "startUpdate" }
  | { readonly kind: "consume" };

export interface HomeControllerState {
  readonly features: readonly FeatureReadModel[];
  readonly portraits: readonly RolePortrait[];
  readonly selectedIndex: number;
  readonly searchQuery: string;
  readonly searching: boolean;
  readonly creating: boolean;
  readonly featureTitle: string;
  readonly actionModalOpen: boolean;
  readonly selectedAction: number;
  readonly tip: string;
  readonly filteredFeatures: readonly FeatureReadModel[];
  readonly updateStatus: UpdateStatusReadModel;
  readonly updateConfirmationOpen: boolean;
  readonly selectedUpdateAction: 0 | 1;
}

export interface HomeControllerActions {
  handleKeyDown(event: {
    name: string;
    ctrl: boolean;
    shift: boolean;
    meta: boolean;
  }): void;
  setFeatureTitle(title: string): void;
  submitFeature(): void;
}
