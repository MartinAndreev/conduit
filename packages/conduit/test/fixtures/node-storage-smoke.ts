import {
  GlobalDatabaseFactory,
  ProjectDatabaseFactory,
} from "../../src/system/storage/factories/database-factories.js";
import { writeFile } from "node:fs/promises";

const projectRoot = process.argv[2];
const globalRoot = process.argv[3];
const resultPath = process.argv[4];
if (!projectRoot || !globalRoot || !resultPath)
  throw new Error("Storage smoke paths required.");

const project = await new ProjectDatabaseFactory(projectRoot).open();
const projectStatement = await project.prepare(
  "SELECT id FROM schema_migrations ORDER BY id",
);
const projectMigrations = (await projectStatement.all()).rows.length;
await projectStatement.finalize();
await project.close();

const global = await new GlobalDatabaseFactory({
  ...process.env,
  XDG_DATA_HOME: globalRoot,
  APPDATA: globalRoot,
}).open();
const globalStatement = await global.prepare(
  "SELECT id FROM schema_migrations ORDER BY id",
);
const globalMigrations = (await globalStatement.all()).rows.length;
await globalStatement.finalize();
await global.close();

await writeFile(
  resultPath,
  JSON.stringify({ projectMigrations, globalMigrations }),
);
