import { readdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import prettier from "prettier";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const rolesDirectory = path.join(root, "skills", "roles");
const outputFile = path.join(
  root,
  "src",
  "domains",
  "roles",
  "assets",
  "role-templates.ts",
);

export async function generateRoleTemplates() {
  const names = (await readdir(rolesDirectory))
    .filter((name) => name.endsWith(".md"))
    .map((name) => name.slice(0, -3))
    .sort();
  const templates = Object.fromEntries(
    await Promise.all(
      names.map(async (name) => [
        name,
        await readFile(path.join(rolesDirectory, `${name}.md`), "utf8"),
      ]),
    ),
  );
  const source = `// Generated from skills/roles/*.md by scripts/generate-role-templates.js. Do not edit manually.\nexport const roleTemplates: Record<string, string> = ${JSON.stringify(templates, null, 2)};\n`;
  await writeFile(
    outputFile,
    await prettier.format(source, { filepath: outputFile }),
  );
}

if (import.meta.main) await generateRoleTemplates();
