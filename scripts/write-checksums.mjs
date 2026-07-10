import { createHash } from "node:crypto";
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const directory = path.resolve("dist/release");
const files = (await readdir(directory))
  .filter((file) => file !== "SHA256SUMS")
  .sort();
const sums = await Promise.all(
  files.map(async (file) => {
    const digest = createHash("sha256")
      .update(await readFile(path.join(directory, file)))
      .digest("hex");
    return `${digest}  ${file}`;
  }),
);
await writeFile(path.join(directory, "SHA256SUMS"), `${sums.join("\n")}\n`);
