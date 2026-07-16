import type { ConduitRuntimeEvent } from "@system/communication/types/runtime-event.js";

export interface RuntimeEventRepository {
  append(event: ConduitRuntimeEvent): Promise<void>;
  loadByRole(
    runId: string,
    roleId: string,
  ): Promise<readonly ConduitRuntimeEvent[]>;
}
