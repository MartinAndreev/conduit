import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";

export interface UpdateScreenState {
  readonly status: UpdateStatusReadModel | undefined;
  readonly frame: number;
  readonly queryError: string | undefined;
}

export type UpdateScreenKeyAction = "home" | "retry" | "quit" | "none";
