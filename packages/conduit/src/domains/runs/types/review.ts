export type ReviewDecision = "approved" | "rejected";

export interface ReviewFinding {
  readonly severity: "info" | "warning" | "error";
  readonly file?: string;
  readonly line?: number;
  readonly message: string;
}

export interface ChangedFile {
  readonly path: string;
  readonly additions: number;
  readonly deletions: number;
}

export interface RunDiffResult {
  readonly diff: string | undefined;
  readonly changedFiles: readonly ChangedFile[];
  readonly totalAdditions: number;
  readonly totalDeletions: number;
}

export interface ReviewResult {
  readonly reviewId: string;
  readonly runId: string;
  readonly featureId: string;
  readonly decision: ReviewDecision;
  readonly findings: readonly ReviewFinding[];
  readonly evidencePaths: readonly string[];
  readonly followUp: string | undefined;
  readonly reviewedAt: string;
}
