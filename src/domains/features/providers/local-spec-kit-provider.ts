import { readFile, readdir, stat, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import type {
  FeatureProvider,
  FeatureReadModel,
  FeatureMetadata,
  FeatureLifecycle,
} from "../types/feature-provider.js";
import { pathExists } from "../../../config.js";

const METADATA_FILE = "metadata.yml";

function serializeMetadata(metadata: FeatureMetadata): string {
  return (
    [
      `lifecycle: ${metadata.lifecycle}`,
      `updatedAt: ${metadata.updatedAt}`,
    ].join("\n") + "\n"
  );
}

function parseMetadata(content: string): FeatureMetadata {
  let lifecycle: FeatureLifecycle = "not_started";
  let updatedAt = new Date().toISOString();

  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^(\w+):\s*(.+)$/);
    if (!match) continue;
    const [, key, value] = match;
    if (key === "lifecycle") lifecycle = value.trim() as FeatureLifecycle;
    if (key === "updatedAt") updatedAt = value.trim();
  }

  return { lifecycle, updatedAt };
}

function deriveTitle(featureId: string): string {
  return featureId
    .replace(/^\d+-/, "")
    .replace(/-/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

export class LocalSpecKitProvider implements FeatureProvider {
  readonly name = "local-spec-kit";
  private specsDir: string;
  private features: Map<string, FeatureReadModel> = new Map();
  private loaded = false;

  constructor(specsDir: string) {
    this.specsDir = specsDir;
  }

  get available(): boolean {
    return true;
  }

  private async ensureLoaded(): Promise<void> {
    if (this.loaded) return;
    await this.loadFeatures();
    this.loaded = true;
  }

  private async loadFeatures(): Promise<void> {
    if (!(await pathExists(this.specsDir))) return;

    const entries = await readdir(this.specsDir);
    for (const entry of entries) {
      const entryPath = path.join(this.specsDir, entry);
      const entryStat = await stat(entryPath).catch(() => null);
      if (!entryStat?.isDirectory()) continue;

      const specFile = path.join(entryPath, "spec.md");
      if (!(await pathExists(specFile))) continue;

      const metadataPath = path.join(entryPath, METADATA_FILE);
      let metadata: FeatureMetadata;
      if (await pathExists(metadataPath)) {
        metadata = parseMetadata(await readFile(metadataPath, "utf8"));
      } else {
        metadata = {
          lifecycle: "not_started",
          updatedAt: new Date().toISOString(),
        };
      }

      const title = deriveTitle(entry);
      this.features.set(entry, {
        id: entry,
        directory: entryPath,
        title,
        metadata,
      });
    }
  }

  async listFeatures(): Promise<readonly FeatureReadModel[]> {
    await this.ensureLoaded();
    return [...this.features.values()];
  }

  async getFeature(id: string): Promise<FeatureReadModel | undefined> {
    await this.ensureLoaded();
    return this.features.get(id);
  }

  async updateMetadata(
    id: string,
    partial: Partial<FeatureMetadata>,
  ): Promise<void> {
    await this.ensureLoaded();
    const feature = this.features.get(id);
    if (!feature) return;

    const updated: FeatureMetadata = {
      lifecycle: partial.lifecycle ?? feature.metadata.lifecycle,
      updatedAt: new Date().toISOString(),
    };

    const metadataPath = path.join(feature.directory, METADATA_FILE);
    await mkdir(feature.directory, { recursive: true });
    await writeFile(metadataPath, serializeMetadata(updated));

    this.features.set(id, {
      ...feature,
      metadata: updated,
    });
  }
}
