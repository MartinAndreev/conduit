import type { ConduitResultRecordV1 } from "../types/agent-protocol.js";

export interface ConduitResultRecordRepository {
  save(record: ConduitResultRecordV1): Promise<void>;
  load(runId: string, role: string): Promise<ConduitResultRecordV1 | undefined>;
}
