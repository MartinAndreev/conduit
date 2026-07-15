export interface GlobalConfigurationMetadataRepository {
  set(key: string, value: unknown): Promise<void>;
  get(key: string): Promise<unknown | undefined>;
}
