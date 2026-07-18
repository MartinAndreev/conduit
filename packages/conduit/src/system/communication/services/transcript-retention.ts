import {
  appendFile,
  mkdir,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { redactSecrets } from "../../storage/security/secret-redaction.js";
import type {
  TranscriptRetentionPolicy,
  TranscriptWriteResult,
} from "../types/transcript.js";

export const defaultTranscriptRetentionPolicy: TranscriptRetentionPolicy = {
  enabled: true,
  retentionDays: 7,
  maxTotalSizeMb: 250,
  maxFileSizeMb: 10,
  retainFailedRunsDays: 30,
};

export class BoundedTranscriptWriter {
  private sizeBytes = 0;
  private truncated = false;
  private initialized = false;
  constructor(
    private readonly transcriptPath: string,
    private readonly policy: TranscriptRetentionPolicy = defaultTranscriptRetentionPolicy,
  ) {}

  async append(content: string): Promise<void> {
    if (!this.policy.enabled || this.truncated) return;
    if (!this.initialized) {
      await mkdir(path.dirname(this.transcriptPath), { recursive: true });
      await writeFile(this.transcriptPath, "", { mode: 0o600 });
      this.initialized = true;
    }
    const sanitized = redactSecrets(content);
    const maximum = Math.max(0, this.policy.maxFileSizeMb) * 1024 * 1024;
    const remaining = maximum - this.sizeBytes;
    if (remaining <= 0) {
      this.truncated = true;
      return;
    }
    const bytes = Buffer.from(sanitized, "utf8");
    const accepted = bytes.subarray(0, remaining);
    await appendFile(this.transcriptPath, accepted);
    this.sizeBytes += accepted.byteLength;
    if (accepted.byteLength < bytes.byteLength) this.truncated = true;
  }

  result(): TranscriptWriteResult {
    return {
      path: this.transcriptPath,
      sizeBytes: this.sizeBytes,
      truncated: this.truncated,
    };
  }
}

export async function cleanupTranscripts(
  root: string,
  policy: TranscriptRetentionPolicy = defaultTranscriptRetentionPolicy,
): Promise<void> {
  const files: {
    path: string;
    size: number;
    modified: number;
    retentionDays: number;
  }[] = [];
  const visit = async (directory: string): Promise<void> => {
    const terminal = await readFile(
      path.join(directory, "terminal.json"),
      "utf8",
    )
      .then((content) => JSON.parse(content) as { status?: string })
      .catch(() => undefined);
    const retentionDays =
      terminal?.status === "failed"
        ? policy.retainFailedRunsDays
        : policy.retentionDays;
    const entries = await readdir(directory, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      const target = path.join(directory, entry.name);
      if (entry.isDirectory()) await visit(target);
      else if (entry.isFile() && entry.name.endsWith(".log")) {
        const details = await stat(target);
        files.push({
          path: target,
          size: details.size,
          modified: details.mtimeMs,
          retentionDays,
        });
      }
    }
  };
  await visit(root);
  const now = Date.now();
  for (const file of files.filter(
    (entry) =>
      entry.modified < now - Math.max(0, entry.retentionDays) * 86_400_000,
  )) {
    await rm(file.path, { force: true });
    file.size = 0;
  }
  let total = files.reduce((sum, file) => sum + file.size, 0);
  const budget = Math.max(0, policy.maxTotalSizeMb) * 1024 * 1024;
  for (const file of files.sort(
    (left, right) => left.modified - right.modified,
  )) {
    if (total <= budget) break;
    await rm(file.path, { force: true });
    total -= file.size;
  }
}
