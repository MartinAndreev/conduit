import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const packageMetadata = JSON.parse(
  await readFile(resolve(packageRoot, "package.json"), "utf8"),
);

if (
  typeof packageMetadata.version !== "string" ||
  !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(
    packageMetadata.version,
  )
) {
  throw new Error("package.json contains an invalid package version");
}

const source = `// Generated from package.json by scripts/generate-version.js. Do not edit.\nexport const conduitVersion = ${JSON.stringify(packageMetadata.version)};\n`;
await writeFile(resolve(packageRoot, "src/generated/version.ts"), source);
