export interface GlobalProfilesTable {
  name: string;
  runner: string | null;
  model: string | null;
  effort: string | null;
  mode: string | null;
  read_only: number | null;
  owns_json: string;
  skill_source: string | null;
  metadata_json: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface ConfigurationDatabase {
  global_profiles: GlobalProfilesTable;
}
