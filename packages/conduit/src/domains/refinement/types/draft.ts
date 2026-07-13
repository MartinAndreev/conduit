export interface RefinementDraft {
  readonly featureId: string;
  readonly story: string;
  readonly testCases: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface DraftField {
  readonly name: string;
  readonly label: string;
  readonly guidance: string;
  readonly required: boolean;
  readonly multiline: boolean;
}
