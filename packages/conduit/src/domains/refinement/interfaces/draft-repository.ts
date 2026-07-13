import type { RefinementDraft } from "../types/draft.js";

export interface DraftRepository {
  save(draft: RefinementDraft): Promise<string>;
  load(featureId: string): Promise<RefinementDraft | null>;
  discard(featureId: string): Promise<boolean>;
  list(): Promise<readonly RefinementDraft[]>;
}
