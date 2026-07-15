import { generateRoleTemplates } from "./generate-role-templates.js";

await generateRoleTemplates();

const result = await Bun.build({
  entrypoints: ["bin/conduit.js"],
  outdir: "dist",
  target: "node",
  format: "esm",
  external: ["@opentui/core"],
  define: { __CONDUIT_STANDALONE__: "false" },
});

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}

console.log(`Built ${result.outputs[0].path}`);
