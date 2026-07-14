import type { SourceVersion } from "../types/source-version.js";

export interface SourceVersionRepository {
  save(version: SourceVersion): Promise<void>;
  load(
    sourcePath: string,
    sourceVersion: string,
  ): Promise<SourceVersion | undefined>;
  listBySource(sourcePath: string): Promise<readonly SourceVersion[]>;
}
