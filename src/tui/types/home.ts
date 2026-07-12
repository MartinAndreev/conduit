import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RolePortrait } from "@domains/roles/interfaces/role-portrait.js";

export type HomeInteraction =
  | { readonly kind: "idle" }
  | { readonly kind: "search"; readonly query: string }
  | { readonly kind: "create"; readonly title: string }
  | { readonly kind: "featureActions"; readonly actionIndex: number };

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
