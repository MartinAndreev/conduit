export interface FeaturePackageVersionRecord {
  readonly id: string;
  readonly featureId: string;
  readonly packageHash: string;
  readonly inputs: readonly string[];
  readonly createdAt: string;
}

export interface HarnessSessionRecord {
  readonly id: string;
  readonly featureId: string;
  readonly packageVersionId: string;
  readonly providerId: string;
  readonly harness: string;
  readonly harnessVersion?: string;
  readonly protocol: string;
  readonly model?: string;
  readonly nativeSessionId?: string;
  readonly status: "active" | "closed" | "unavailable" | "superseded";
  readonly supersedesSessionId?: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface HarnessTurnRecord {
  readonly id: string;
  readonly sessionId: string;
  readonly assignmentId: string;
  readonly kind: "assignment" | "clarification" | "review-feedback";
  readonly status: "running" | "completed" | "failed" | "cancelled";
  readonly startedAt: string;
  readonly completedAt?: string;
}

export interface DiagnosticArtifactRecord {
  readonly id: string;
  readonly runId?: string;
  readonly roleId?: string;
  readonly kind: "transcript" | "launch" | "other";
  readonly path: string;
  readonly sizeBytes: number;
  readonly truncated: boolean;
  readonly createdAt: string;
  readonly expiresAt: string;
}
