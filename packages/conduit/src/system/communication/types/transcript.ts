export interface TranscriptRetentionPolicy {
  readonly enabled: boolean;
  readonly retentionDays: number;
  readonly maxTotalSizeMb: number;
  readonly maxFileSizeMb: number;
  readonly retainFailedRunsDays: number;
}

export interface TranscriptWriteResult {
  readonly path: string;
  readonly sizeBytes: number;
  readonly truncated: boolean;
}
