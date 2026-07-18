export type ResumeEligibilityState = "checking" | "resumable" | "not-resumable";

export interface ResumeEligibility {
  readonly state: ResumeEligibilityState;
  readonly reason?: string;
  readonly preservedRoles: readonly string[];
  readonly retryRoles: readonly string[];
  readonly reconstructRoles: readonly string[];
}
