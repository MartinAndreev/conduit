import type { Generated } from "kysely";

export interface RunEventsTable {
  id: Generated<number>;
  run_id: string;
  role_id: string;
  sequence: number;
  event_type: string;
  timestamp: string;
  payload_json: string;
}

export interface ReviewResultsTable {
  run_id: string;
  review_id: string;
  feature_id: string;
  decision: string;
  findings_json: string;
  evidence_paths_json: string;
  follow_up: string | null;
  reviewed_at: string;
}

export interface RunSnapshotsTable {
  run_id: string;
  snapshot_json: string;
  status: string;
  version: number;
  updated_at: string;
}

export interface RunRecoveryTable {
  run_id: string;
  state: string;
  diagnostic: string | null;
  updated_at: string;
}

export interface FeaturePackageVersionsTable {
  package_version_id: string;
  feature_id: string;
  package_hash: string;
  inputs_json: string;
  created_at: string;
}

export interface HarnessSessionsTable {
  session_id: string;
  feature_id: string;
  package_version_id: string;
  provider_id: string;
  harness: string;
  harness_version: string | null;
  protocol: string;
  model: string | null;
  native_session_id: string | null;
  status: string;
  supersedes_session_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface HarnessTurnsTable {
  turn_id: string;
  session_id: string;
  assignment_id: string;
  kind: string;
  status: string;
  started_at: string;
  completed_at: string | null;
}

export interface RuntimeEventsTable {
  id: Generated<number>;
  run_id: string;
  role_id: string;
  sequence: number;
  event_json: string;
  received_at: string;
}

export interface ResultRecordsTable {
  run_id: string;
  role_id: string;
  record_json: string;
  received_at: string;
}

export interface RoleWorkspaceSlotsTable {
  repository_id: string;
  role_key: string;
  generation: number;
  workspace_path: string;
  owning_run_id: string;
  state: string;
  starting_head: string;
  package_hash: string;
  assignment_hash: string;
  worktree_head: string | null;
  branch_name: string;
  lease_owner: string | null;
  fencing_token: number;
  leased_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface RoleWorkspaceGenerationsTable {
  repository_id: string;
  role_key: string;
  generation: number;
  workspace_path: string;
  owning_run_id: string;
  starting_head: string;
  package_hash: string;
  assignment_hash: string;
  branch_name: string;
  branch_oid: string | null;
  outcome: string | null;
  promotion_oid: string | null;
  created_at: string;
  completed_at: string | null;
}

export interface DiagnosticArtifactsTable {
  artifact_id: string;
  run_id: string | null;
  role_id: string | null;
  kind: string;
  path: string;
  size_bytes: number;
  truncated: number;
  created_at: string;
  expires_at: string;
}

export interface RunsDatabase {
  run_events: RunEventsTable;
  review_results: ReviewResultsTable;
  run_snapshots: RunSnapshotsTable;
  run_recovery: RunRecoveryTable;
  feature_package_versions: FeaturePackageVersionsTable;
  harness_sessions: HarnessSessionsTable;
  harness_turns: HarnessTurnsTable;
  runtime_events: RuntimeEventsTable;
  result_records: ResultRecordsTable;
  role_workspace_slots: RoleWorkspaceSlotsTable;
  role_workspace_generations: RoleWorkspaceGenerationsTable;
  diagnostic_artifacts: DiagnosticArtifactsTable;
}
