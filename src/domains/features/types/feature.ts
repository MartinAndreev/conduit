export type FeatureLifecycle = "not_started" | "in_progress" | "implemented";

export interface FeatureMetadata {
  readonly lifecycle: FeatureLifecycle;
  readonly updatedAt: string;
}

export interface FeatureReadModel {
  readonly id: string;
  readonly directory: string;
  readonly title: string;
  readonly metadata: FeatureMetadata;
}

export interface Feature {
  id: string;
  directory: string;
}
