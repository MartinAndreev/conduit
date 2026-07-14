import { readFile, rm } from "node:fs/promises";
import { containsSecret } from "./secret-redaction.js";

export async function verifyBackupContainsNoSecrets(
  backupPath: string,
): Promise<void> {
  const bytes = await readFile(backupPath);
  if (!containsSecret(bytes.toString("latin1"))) return;
  await rm(backupPath, { force: true });
  throw new Error(
    "Pre-migration backup failed the secret-redaction check and was removed.",
  );
}
