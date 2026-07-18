import type {
  DiagnosticArtifactRecord,
  FeaturePackageVersionRecord,
  HarnessSessionRecord,
  HarnessTurnRecord,
} from "../types/harness-runtime-state.js";

export interface HarnessRuntimeStateRepository {
  savePackageVersion(record: FeaturePackageVersionRecord): Promise<void>;
  findPackageVersion(
    packageHash: string,
  ): Promise<FeaturePackageVersionRecord | undefined>;
  saveSession(record: HarnessSessionRecord): Promise<void>;
  loadSession(sessionId: string): Promise<HarnessSessionRecord | undefined>;
  saveTurn(record: HarnessTurnRecord): Promise<void>;
  saveDiagnosticArtifact(record: DiagnosticArtifactRecord): Promise<void>;
}
