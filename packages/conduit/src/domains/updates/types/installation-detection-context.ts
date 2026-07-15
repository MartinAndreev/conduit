export interface InstallationDetectionContext {
  readonly standaloneBuild: boolean;
  readonly executablePath: string;
  readonly entryPath?: string;
  readonly platform: NodeJS.Platform;
  readonly architecture: string;
}
