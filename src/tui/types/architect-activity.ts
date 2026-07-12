import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";

export interface ArchitectActivityViewModel {
  readonly events: readonly ArchitectEvent[];
  readonly uniqueFiles: readonly string[];
  readonly expandedIndex: number | null;
  readonly selectedFileIndex: number;
  readonly selectedDiff: string | undefined;
  readonly loading: boolean;
  readonly error: string | null;
}
