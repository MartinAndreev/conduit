export type SourceVersion = Readonly<{
  sourcePath: string;
  sourceVersion: string;
  contentChecksum: string;
  observedAt: string;
  metadata: Readonly<Record<string, string>>;
}>;
