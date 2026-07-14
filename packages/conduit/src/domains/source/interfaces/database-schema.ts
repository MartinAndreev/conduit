export interface SourceVersionsTable {
  source_path: string;
  source_version: string;
  content_checksum: string;
  observed_at: string;
  metadata_json: string;
}

export interface SourceDatabase {
  source_versions: SourceVersionsTable;
}
