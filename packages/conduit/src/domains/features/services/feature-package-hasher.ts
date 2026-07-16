import { createHash } from "node:crypto";
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";

const ROOT_FILES = ["story.md", "spec.md", "plan.md", "tasks.md", "test-cases.md"] as const;
const CONTRACTS_DIR = "contracts";

export interface FeaturePackageHashInput {
  readonly packageRoot: string;
  readonly ownershipFiles?: readonly string[];
}

export interface FeaturePackageHashResult {
  readonly algorithm: "sha256";
  readonly hash: string;
  readonly files: readonly string[];
  readonly lineEndings: "lf-normalized";
}

export async function hashFeaturePackage(
  input: FeaturePackageHashInput,
): Promise<FeaturePackageHashResult> {
  const root = path.resolve(input.packageRoot);
  const discovered = await discoverPackageFiles(root, input.ownershipFiles ?? []);
  const hash = createHash("sha256");

  for (const relativePath of discovered) {
    const absolutePath = path.join(root, relativePath);
    const bytes = await readFile(absolutePath, "utf8");
    hash.update(relativePath, "utf8");
    hash.update("\0");
    hash.update(normalizeLineEndings(bytes), "utf8");
    hash.update("\0");
  }

  return {
    algorithm: "sha256",
    hash: hash.digest("hex"),
    files: discovered,
    lineEndings: "lf-normalized",
  };
}

async function discoverPackageFiles(
  root: string,
  ownershipFiles: readonly string[],
): Promise<readonly string[]> {
  const candidates = new Set<string>();

  for (const file of ROOT_FILES) {
    if (await isFile(path.join(root, file))) candidates.add(file);
  }

  const contractsRoot = path.join(root, CONTRACTS_DIR);
  if (await isDirectory(contractsRoot)) {
    for (const file of await walkFiles(contractsRoot)) {
      candidates.add(path.join(CONTRACTS_DIR, path.relative(contractsRoot, file)));
    }
  }

  for (const file of ownershipFiles) {
    const relativePath = normalizeRelativePackagePath(file);
    if (relativePath && (await isFile(path.join(root, relativePath)))) {
      candidates.add(relativePath);
    }
  }

  return [...candidates].sort((a, b) => a.localeCompare(b));
}

async function walkFiles(directory: string): Promise<readonly string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    const absolutePath = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...(await walkFiles(absolutePath)));
    if (entry.isFile()) files.push(absolutePath);
  }
  return files;
}

async function isFile(absolutePath: string): Promise<boolean> {
  try {
    return (await stat(absolutePath)).isFile();
  } catch {
    return false;
  }
}

async function isDirectory(absolutePath: string): Promise<boolean> {
  try {
    return (await stat(absolutePath)).isDirectory();
  } catch {
    return false;
  }
}

function normalizeRelativePackagePath(value: string): string | undefined {
  const normalized = path.posix.normalize(value.replaceAll(path.sep, "/"));
  if (normalized.startsWith("../") || normalized === ".." || path.isAbsolute(value)) {
    return undefined;
  }
  return normalized;
}

function normalizeLineEndings(value: string): string {
  return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
}
