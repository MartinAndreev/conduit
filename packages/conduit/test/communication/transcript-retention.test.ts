import assert from "node:assert/strict";
import test from "node:test";
import {
  mkdtemp,
  readFile,
  rm,
  stat,
  utimes,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {
  BoundedTranscriptWriter,
  cleanupTranscripts,
} from "../../src/system/communication/services/transcript-retention.js";

test("transcripts are secret-redacted, append-only, and capped", async () => {
  const directory = await mkdtemp(
    path.join(os.tmpdir(), "conduit-transcript-"),
  );
  const transcript = path.join(directory, "role.log");
  try {
    const writer = new BoundedTranscriptWriter(transcript, {
      enabled: true,
      retentionDays: 7,
      maxTotalSizeMb: 1,
      maxFileSizeMb: 0.0001,
      retainFailedRunsDays: 30,
    });
    await writer.append("first OPENAI_API_KEY=sk-secret-value\n");
    await writer.append("x".repeat(1_000));
    const result = writer.result();
    assert.equal(result.truncated, true);
    assert.ok(result.sizeBytes <= 105);
    assert.doesNotMatch(await readFile(transcript, "utf8"), /sk-secret-value/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("transcript cleanup applies age and total budget", async () => {
  const directory = await mkdtemp(
    path.join(os.tmpdir(), "conduit-transcript-clean-"),
  );
  try {
    const old = path.join(directory, "old.log");
    const current = path.join(directory, "current.log");
    await writeFile(old, "old");
    await writeFile(current, "current");
    const past = new Date(Date.now() - 10 * 86_400_000);
    await utimes(old, past, past);
    await cleanupTranscripts(directory, {
      enabled: true,
      retentionDays: 7,
      maxTotalSizeMb: 1,
      maxFileSizeMb: 1,
      retainFailedRunsDays: 30,
    });
    await assert.rejects(stat(old));
    assert.equal(await readFile(current, "utf8"), "current");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
