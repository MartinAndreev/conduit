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

export interface FeatureProvider {
  readonly name: string;
  readonly available: boolean;
  listFeatures(): Promise<readonly FeatureReadModel[]>;
  getFeature(id: string): Promise<FeatureReadModel | undefined>;
  updateMetadata(id: string, metadata: Partial<FeatureMetadata>): Promise<void>;
}
