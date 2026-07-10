import { generateRoleTemplates } from "./generate-role-templates.js";

await generateRoleTemplates();

const targets = {
  "linux-x64": {
    target: "bun-linux-x64",
    outfile: "dist/release/conduit-linux-x64",
    libc: "glibc",
  },
  "darwin-x64": {
    target: "bun-darwin-x64",
    outfile: "dist/release/conduit-darwin-x64",
  },
  "darwin-arm64": {
    target: "bun-darwin-arm64",
    outfile: "dist/release/conduit-darwin-arm64",
  },
  "windows-x64": {
    target: "bun-windows-x64",
    outfile: "dist/release/conduit-windows-x64.exe",
  },
};

const name = Bun.argv[2];
const build = targets[name];
if (!build)
  throw new Error(`Choose a target: ${Object.keys(targets).join(", ")}`);

const result = await Bun.build({
  entrypoints: ["bin/conduit.js"],
  compile: { target: build.target, outfile: build.outfile },
  define: build.libc
    ? { "process.env.OPENTUI_LIBC": JSON.stringify(build.libc) }
    : {},
});

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}

console.log(`Built ${build.outfile}`);
