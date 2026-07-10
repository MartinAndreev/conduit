import { createHash } from "node:crypto";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { pathExists } from "./config.js";
import { roleTemplates } from "./role-templates.js";

const sha256 = (content) => createHash("sha256").update(content).digest("hex");

export async function resolveSkill({
  projectRoot,
  roleName,
  role,
  builtinRoot,
  allowNetwork = false,
}) {
  const source = role.skill?.source ?? `builtin:${roleName}`;
  if (source.startsWith("builtin:")) {
    const builtin = source.slice("builtin:".length);
    const content = await readFile(
      path.join(builtinRoot, `${builtin}.md`),
      "utf8",
    ).catch(() => roleTemplates[builtin]);
    if (!content) throw new Error(`Unknown built-in role skill: ${builtin}`);
    return { source, content, verified: true };
  }
  if (source.startsWith("file:")) {
    const file = path.resolve(projectRoot, source.slice("file:".length));
    if (!(await pathExists(file)))
      throw new Error(`Skill file does not exist: ${file}`);
    return { source, content: await readFile(file, "utf8"), verified: true };
  }
  if (!source.startsWith("https://"))
    throw new Error(`Unsupported skill source for ${roleName}: ${source}`);
  if (!role.skill.sha256)
    throw new Error(`Remote skill ${roleName} requires a sha256 pin.`);
  const cacheFile = path.join(
    projectRoot,
    ".conduit",
    "cache",
    `${sha256(source)}.md`,
  );
  if (await pathExists(cacheFile)) {
    const content = await readFile(cacheFile, "utf8");
    if (sha256(content) === role.skill.sha256)
      return { source, content, verified: true, cached: true };
  }
  if (!allowNetwork)
    throw new Error(
      `Remote skill ${roleName} is not cached. Re-run with --fetch-skills after reviewing its URL and SHA-256.`,
    );
  const response = await fetch(source, { redirect: "error" });
  if (!response.ok)
    throw new Error(
      `Could not fetch remote skill ${roleName}: ${response.status}`,
    );
  const content = await response.text();
  if (sha256(content) !== role.skill.sha256)
    throw new Error(`SHA-256 mismatch for remote skill ${roleName}.`);
  await mkdir(path.dirname(cacheFile), { recursive: true });
  await writeFile(cacheFile, content);
  return { source, content, verified: true, cached: false };
}

export { sha256 };
