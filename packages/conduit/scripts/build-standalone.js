import { generateRoleTemplates } from "./generate-role-templates.js";

await generateRoleTemplates();

const targets = {
  "linux-x64": {
    target: "bun-linux-x64",
    outfile: "dist/release/conduit-linux-x64",
    libc: "glibc",
    binding: "@tursodatabase/database-linux-x64-gnu",
  },
  "linux-arm64": {
    target: "bun-linux-arm64",
    outfile: "dist/release/conduit-linux-arm64",
    libc: "glibc",
    binding: "@tursodatabase/database-linux-arm64-gnu",
  },
  "darwin-arm64": {
    target: "bun-darwin-arm64",
    outfile: "dist/release/conduit-darwin-arm64",
    binding: "@tursodatabase/database-darwin-arm64",
  },
  "windows-x64": {
    target: "bun-windows-x64",
    outfile: "dist/release/conduit-windows-x64.exe",
    binding: "@tursodatabase/database-win32-x64-msvc",
  },
};

const name = Bun.argv[2];
const build = targets[name];
if (!build) {
  const unsupported =
    name === "darwin-x64" || name?.includes("musl") || name?.includes("alpine");
  throw new Error(
    unsupported
      ? `${name} is unsupported: Feature 002 validates glibc Linux, macOS ARM64, and Windows x64 only.`
      : `Choose a target: ${Object.keys(targets).join(", ")}`,
  );
}

const standaloneTursoModule = `
import { DatabasePromise, SqliteError } from "@tursodatabase/database-common";
const { Database: NativeDatabase } = require("${build.binding}");

class Database extends DatabasePromise {
  constructor(path, options = {}) {
    super(new NativeDatabase(path, options));
  }
}

async function connect(path, options = {}) {
  const database = new Database(path, options);
  await database.connect();
  return database;
}

export { connect, Database, SqliteError };
export const nativeBindingPackage = "${build.binding}";
`;

const tursoBindingPlugin = {
  name: "conduit-standalone-turso-binding",
  setup(builder) {
    builder.onResolve({ filter: /^@tursodatabase\/database$/ }, () => ({
      path: "standalone-turso",
      namespace: "conduit",
    }));
    builder.onLoad(
      { filter: /^standalone-turso$/, namespace: "conduit" },
      () => ({ contents: standaloneTursoModule, loader: "js" }),
    );
  },
};

const result = await Bun.build({
  entrypoints: ["bin/conduit.js"],
  compile: { target: build.target, outfile: build.outfile },
  plugins: [tursoBindingPlugin],
  define: build.libc
    ? {
        __CONDUIT_STANDALONE__: "true",
        "process.env.OPENTUI_LIBC": JSON.stringify(build.libc),
        "process.env.CONDUIT_TURSO_BINDING": JSON.stringify(build.binding),
      }
    : {
        __CONDUIT_STANDALONE__: "true",
        "process.env.CONDUIT_TURSO_BINDING": JSON.stringify(build.binding),
      },
});

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}

console.log(`Built ${build.outfile}`);
