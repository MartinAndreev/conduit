import { createHash } from "node:crypto";
import { UpdateIntegrityError } from "../errors/update-errors.js";

const CHECKSUM_LINE =
  /^([a-fA-F0-9]{64}) {2}([A-Za-z0-9][A-Za-z0-9._-]{0,199})$/;

export function expectedSha256(
  checksumDocument: string,
  assetName: string,
): string {
  const matches: string[] = [];
  for (const line of checksumDocument.split(/\r?\n/)) {
    if (!line) continue;
    const parsed = CHECKSUM_LINE.exec(line);
    if (!parsed)
      throw new UpdateIntegrityError(
        "MALFORMED_CHECKSUM",
        "The release checksum document is malformed.",
      );
    if (parsed[2] === assetName) matches.push(parsed[1]!.toLowerCase());
  }
  if (matches.length === 0)
    throw new UpdateIntegrityError(
      "MISSING_CHECKSUM",
      "The expected release checksum is missing.",
    );
  if (matches.length !== 1)
    throw new UpdateIntegrityError(
      "DUPLICATE_CHECKSUM",
      "The release checksum is duplicated.",
    );
  return matches[0]!;
}

export function sha256(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}
