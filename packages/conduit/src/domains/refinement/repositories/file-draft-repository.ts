import { mkdir, readFile, writeFile, rm, readdir } from "node:fs/promises";
import path from "node:path";
import type { RefinementDraft } from "../types/draft.js";
import type { DraftRepository } from "../interfaces/draft-repository.js";

export class FileDraftRepository implements DraftRepository {
  private readonly draftsDir: string;

  constructor(projectRoot: string, stateDir: string = ".conduit") {
    this.draftsDir = path.join(projectRoot, stateDir, "drafts");
  }

  async save(draft: RefinementDraft): Promise<string> {
    await mkdir(this.draftsDir, { recursive: true });
    const draftPath = this.getDraftPath(draft.featureId);
    await writeFile(draftPath, JSON.stringify(draft, null, 2));
    return draftPath;
  }

  async load(featureId: string): Promise<RefinementDraft | null> {
    try {
      const draftPath = this.getDraftPath(featureId);
      const content = await readFile(draftPath, "utf8");
      return JSON.parse(content) as RefinementDraft;
    } catch {
      return null;
    }
  }

  async discard(featureId: string): Promise<boolean> {
    try {
      const draftPath = this.getDraftPath(featureId);
      await rm(draftPath, { force: true });
      return true;
    } catch {
      return false;
    }
  }

  async list(): Promise<readonly RefinementDraft[]> {
    try {
      await mkdir(this.draftsDir, { recursive: true });
      const files = await readdir(this.draftsDir);
      const drafts: RefinementDraft[] = [];

      for (const file of files) {
        if (file.endsWith(".json")) {
          try {
            const content = await readFile(
              path.join(this.draftsDir, file),
              "utf8",
            );
            drafts.push(JSON.parse(content) as RefinementDraft);
          } catch {
            // Skip invalid draft files
          }
        }
      }

      return drafts;
    } catch {
      return [];
    }
  }

  private getDraftPath(featureId: string): string {
    return path.join(this.draftsDir, `${featureId}.json`);
  }
}
