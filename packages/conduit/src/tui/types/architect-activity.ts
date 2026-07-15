import type { ArchitectEvent } from "@domains/refinement/types/architect-event.js";
import type { Theme } from "@tui/theme.js";

export interface ArchitectActivityProps extends ArchitectActivityViewModel {
  readonly theme: Theme;
  readonly featureId: string;
  readonly running?: boolean;
}

export interface ArchitectActivityViewModel {
  readonly events: readonly ArchitectEvent[];
  readonly uniqueFiles: readonly string[];
  readonly expandedIndex: number | null;
  readonly selectedFileIndex: number;
  readonly selectedDiff: string | undefined;
  readonly loading: boolean;
  readonly error: string | null;
}
