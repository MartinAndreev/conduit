import { rename } from "node:fs/promises";
import type { ExecutableReplacer } from "../interfaces/executable-replacer.js";

export class AtomicExecutableReplacer implements ExecutableReplacer {
  async replace(stagedExecutable: string, destination: string): Promise<void> {
    await rename(stagedExecutable, destination);
  }
}
