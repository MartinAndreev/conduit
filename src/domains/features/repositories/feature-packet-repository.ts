import { mkdir, writeFile, readFile, readdir } from "node:fs/promises";
import path from "node:path";
import type { Config } from "../../configuration/types/config.js";
import type { Feature } from "../types/feature.js";

const slugify = (text: string): string =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 48) || "feature";

export async function nextFeatureId(specsDir: string): Promise<string> {
  const items = await readdir(specsDir, { withFileTypes: true }).catch(
    () => [] as import("node:fs").Dirent[],
  );
  const ids = items
    .filter((item) => item.isDirectory())
    .map((item) => Number(item.name.slice(0, 3)))
    .filter(Number.isFinite);
  return String(Math.max(0, ...ids) + 1).padStart(3, "0");
}

export async function createFeature({
  projectRoot,
  config,
  title,
}: {
  projectRoot: string;
  config: Config;
  title: string;
}): Promise<Feature> {
  const specsDir = path.join(projectRoot, config.specsDir);
  await mkdir(specsDir, { recursive: true });
  const id = await nextFeatureId(specsDir);
  const directory = path.join(specsDir, `${id}-${slugify(title)}`);
  await mkdir(path.join(directory, "contracts"), { recursive: true });
  await writeFile(
    path.join(directory, "spec.md"),
    `# ${title}\n\n## Problem\n\nDescribe the user problem and desired outcome.\n\n## Acceptance criteria\n\n- [ ] Define observable success criteria.\n\n## Open questions\n\n- [ ] Resolve before implementation.\n`,
  );
  await writeFile(
    path.join(directory, "plan.md"),
    `# Implementation plan\n\n## Architecture decision\n\nPending architect approval.\n\n## Ownership\n\n| Role | Directories | Deliverable |\n| --- | --- | --- |\n| frontend | TBD | UI implementation |\n| backend | TBD | API implementation |\n| documentation | TBD | User, operator, and developer documentation |\n`,
  );
  await writeFile(
    path.join(directory, "tasks.md"),
    `# Tasks\n\n- [ ] T001 Define contracts and approve the spec\n- [ ] T002 Frontend implementation\n- [ ] T003 Backend implementation\n- [ ] T004 Integration and tests\n- [ ] T005 Documentation and release notes\n`,
  );
  await writeFile(
    path.join(directory, "contracts", "README.md"),
    `# Contracts\n\nPlace API, event, shared-type, and UI-state contracts here. These files are the handoff boundary for parallel workers.\n`,
  );
  await writeTestCases({ directory }, "");
  return { id, directory };
}

export async function findFeature({
  projectRoot,
  config,
  featureId,
}: {
  projectRoot: string;
  config: Config;
  featureId: string;
}): Promise<Feature> {
  const specsDir = path.join(projectRoot, config.specsDir);
  const entries = await readdir(specsDir, { withFileTypes: true }).catch(
    () => [] as import("node:fs").Dirent[],
  );
  const match = entries.find(
    (entry) => entry.isDirectory() && entry.name.startsWith(`${featureId}-`),
  );
  if (!match)
    throw new Error(`Feature ${featureId} was not found in ${specsDir}.`);
  return { id: featureId, directory: path.join(specsDir, match.name) };
}

export async function writeStory(
  feature: Feature,
  story: string,
): Promise<string> {
  const content = `# Story\n\n${story.trim()}\n`;
  await writeFile(path.join(feature.directory, "story.md"), content);
  return path.join(feature.directory, "story.md");
}

export async function writeTestCases(
  feature: Pick<Feature, "directory">,
  testCases: string,
): Promise<string> {
  const content = `# QA test cases\n\n${testCases.trim() || "- [ ] Define the acceptance and regression tests before implementation."}\n`;
  await writeFile(path.join(feature.directory, "test-cases.md"), content);
  return path.join(feature.directory, "test-cases.md");
}

export async function readStory(feature: Feature): Promise<string> {
  const file = path.join(feature.directory, "story.md");
  const content = await readFile(file, "utf8").catch(() => undefined);
  if (!content)
    throw new Error(
      `Feature ${feature.id} has no saved story. Run \`conduit refine ${feature.id}\` first.`,
    );
  return content.replace(/^# Story\s*/i, "").trim();
}
