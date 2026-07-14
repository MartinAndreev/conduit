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

export interface RunsDatabase {
  run_events: RunEventsTable;
  review_results: ReviewResultsTable;
  run_snapshots: RunSnapshotsTable;
  run_recovery: RunRecoveryTable;
}
