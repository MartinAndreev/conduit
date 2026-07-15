import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import type { ConduitResultRecordRepository } from "../interfaces/conduit-result-record-repository.js";
import type { ConduitResultRecordV1 } from "../types/agent-protocol.js";
import { redactPersistedValue } from "@system/storage/security/secret-redaction.js";

export class FileConduitResultRecordRepository implements ConduitResultRecordRepository {
  constructor(private readonly runsDirectory: string) {}

  async save(record: ConduitResultRecordV1): Promise<void> {
    const file = this.recordPath(record.runId, record.role);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(
      file,
      `${JSON.stringify(redactPersistedValue(record), null, 2)}\n`,
    );
  }

  async load(
    runId: string,
    role: string,
  ): Promise<ConduitResultRecordV1 | undefined> {
    try {
      return JSON.parse(
        await readFile(this.recordPath(runId, role), "utf8"),
      ) as ConduitResultRecordV1;
    } catch {
      return undefined;
    }
  }

  private recordPath(runId: string, role: string): string {
    return path.join(this.runsDirectory, runId, `${role}-result.json`);
  }
}
