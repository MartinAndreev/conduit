import type { FeatureMetadata, FeatureReadModel } from "../types/feature.js";

export interface FeatureProvider {
  readonly name: string;
  readonly available: boolean;
  listFeatures(): Promise<readonly FeatureReadModel[]>;
  getFeature(id: string): Promise<FeatureReadModel | undefined>;
  updateMetadata(id: string, metadata: Partial<FeatureMetadata>): Promise<void>;
}
