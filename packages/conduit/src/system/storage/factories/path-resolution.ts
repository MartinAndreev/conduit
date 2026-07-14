import { homedir, platform } from "node:os";
import { isAbsolute, join, resolve } from "node:path";
import type { DatabasePathSet } from "../types/database.js";

export function resolveProjectDatabasePaths(
  projectRoot: string,
  stateDirectory?: string,
): DatabasePathSet {
  const configured = stateDirectory ?? ".conduit";
  const directory = isAbsolute(configured)
    ? configured
    : resolve(projectRoot, configured);
  return { directory, databasePath: join(directory, "state.db") };
}

export function resolveGlobalDatabasePaths(
  environment: NodeJS.ProcessEnv = process.env,
): DatabasePathSet {
  let directory: string;
  if (platform() === "win32") {
    directory = join(
      environment.APPDATA ?? join(homedir(), "AppData", "Roaming"),
      "conduit",
    );
  } else if (platform() === "darwin") {
    directory = join(homedir(), "Library", "Application Support", "conduit");
  } else {
    directory = join(
      environment.XDG_DATA_HOME ?? join(homedir(), ".local", "share"),
      "conduit",
    );
  }
  return { directory, databasePath: join(directory, "global.db") };
}
