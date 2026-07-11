import { readFile, writeFile, mkdir, access, cp } from "node:fs/promises";
import { constants } from "node:fs";
import path from "node:path";
import type {
  Config,
  RoleConfig,
} from "./domains/configuration/types/config.js";

export const CONFIG_FILE = "conduit.yml" as const;

export const defaultConfig: Config = {
  version: 1,
  specsDir: "specs",
  stateDir: ".conduit",
  roles: {
    architect: {
      description:
        "Turns a story into an approved, implementation-ready specification.",
      runner: "codex",
      mode: "primary",
      skill: { source: "file:.conduit/roles/architect.md" },
    },
    researcher: {
      description:
        "Investigates the repository and reports evidence without changing code.",
      runner: "opencode",
      mode: "subagent",
      readOnly: true,
      skill: { source: "file:.conduit/roles/researcher.md" },
    },
    frontend: {
      description:
        "Implements approved user-interface work within its owned paths.",
      runner: "opencode",
      mode: "subagent",
      owns: ["apps/web", "packages/ui"],
      skill: { source: "file:.conduit/roles/frontend.md" },
    },
    backend: {
      description: "Implements approved API, service, and data-contract work.",
      runner: "opencode",
      mode: "subagent",
      owns: ["apps/api", "packages/contracts"],
      skill: { source: "file:.conduit/roles/backend.md" },
    },
    qa: {
      description:
        "Converts approved cases into tests and reports reproducible defects.",
      runner: "opencode",
      mode: "subagent",
      owns: ["tests", "e2e"],
      skill: { source: "file:.conduit/roles/qa.md" },
    },
    documentation: {
      description:
        "Writes and verifies user, operator, and developer documentation.",
      runner: "opencode",
      mode: "subagent",
      owns: ["docs", "README.md"],
      skill: { source: "file:.conduit/roles/documentation.md" },
    },
    reviewer: {
      description:
        "Independently checks the integrated change against the approved spec.",
      runner: "codex",
      mode: "primary",
      readOnly: true,
      skill: { source: "file:.conduit/roles/reviewer.md" },
    },
  },
};

export function serializeConfig(config: Config = defaultConfig): string {
  const lines = [
    "version: 1",
    "specsDir: specs",
    "stateDir: .conduit",
    "roles:",
  ];
  for (const [name, role] of Object.entries(config.roles)) {
    lines.push(`  ${name}:`);
    if (role.description) lines.push(`    description: "${role.description}"`);
    lines.push(`    runner: ${role.runner}`);
    lines.push(`    mode: ${role.mode}`);
    if (role.model) lines.push(`    model: ${role.model}`);
    if (role.readOnly) lines.push("    readOnly: true");
    if (role.owns?.length) lines.push(`    owns: [${role.owns.join(", ")}]`);
    lines.push("    skill:");
    lines.push(`      source: ${role.skill.source}`);
    if (role.skill.sha256) lines.push(`      sha256: ${role.skill.sha256}`);
  }
  return `${lines.join("\n")}\n`;
}

function scalar(value: string): string | number | boolean | string[] {
  if (value === "true") return true;
  if (value === "false") return false;
  if (/^\d+$/.test(value)) return Number(value);
  if (value.startsWith("[") && value.endsWith("]"))
    return value
      .slice(1, -1)
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  return value.replace(/^['"]|['"]$/g, "");
}

export function parseConfig(yaml: string): Config {
  const config = { ...defaultConfig, roles: {} as Record<string, RoleConfig> };
  let roleName: string | undefined;
  let inSkill = false;
  for (const rawLine of yaml.split(/\r?\n/)) {
    if (!rawLine.trim() || rawLine.trimStart().startsWith("#")) continue;
    const indent = rawLine.length - rawLine.trimStart().length;
    const line = rawLine.trim();
    const match = line.match(/^([\w-]+):(?:\s*(.*))?$/);
    if (!match) continue;
    const [, key, rawValue] = match;
    if (indent === 0) {
      if (key !== "roles")
        (config as unknown as Record<string, unknown>)[key] = scalar(
          rawValue ?? "",
        );
      roleName = undefined;
      inSkill = false;
    } else if (indent === 2) {
      roleName = key;
      config.roles[roleName] = { skill: { source: "" }, runner: "", mode: "" };
      inSkill = false;
    } else if (indent === 4 && roleName) {
      if (key === "skill") inSkill = true;
      else {
        (config.roles[roleName] as unknown as Record<string, unknown>)[key] =
          scalar(rawValue ?? "");
        inSkill = false;
      }
    } else if (indent === 6 && roleName && inSkill) {
      (config.roles[roleName].skill as Record<string, unknown>)[key] = scalar(
        rawValue ?? "",
      );
    }
  }
  return config;
}

export async function pathExists(target: string): Promise<boolean> {
  try {
    await access(target, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export async function loadConfig(projectRoot: string): Promise<Config> {
  const file = path.join(projectRoot, CONFIG_FILE);
  if (!(await pathExists(file)))
    throw new Error(
      `No ${CONFIG_FILE} found in ${projectRoot}. Run \`conduit init\` first.`,
    );
  return parseConfig(await readFile(file, "utf8"));
}

export async function initializeProject(
  projectRoot: string,
  templateRoot: string,
  embeddedTemplates?: Record<string, string>,
): Promise<{ createdConfig: boolean; configFile: string }> {
  const configFile = path.join(projectRoot, CONFIG_FILE);
  const conduitDir = path.join(projectRoot, ".conduit");
  await mkdir(conduitDir, { recursive: true });
  await mkdir(path.join(conduitDir, "roles"), { recursive: true });
  await mkdir(path.join(projectRoot, "specs"), { recursive: true });
  const configExisted = await pathExists(configFile);
  if (!configExisted) await writeFile(configFile, serializeConfig());
  else {
    const config = parseConfig(await readFile(configFile, "utf8"));
    if (!config.roles.documentation) {
      const documentation = serializeConfig({
        ...config,
        roles: { documentation: defaultConfig.roles.documentation },
      }).split("roles:\n")[1];
      const existing = (await readFile(configFile, "utf8")).replace(/\s*$/, "");
      await writeFile(configFile, `${existing}\n${documentation}`);
    }
  }
  const rolesSource = path.join(templateRoot, "roles");
  for (const name of Object.keys(defaultConfig.roles)) {
    const target = path.join(conduitDir, "roles", `${name}.md`);
    if (!(await pathExists(target))) {
      if (embeddedTemplates?.[name])
        await writeFile(target, embeddedTemplates[name]);
      else await cp(path.join(rolesSource, `${name}.md`), target);
    }
  }
  const ignoreFile = path.join(projectRoot, ".gitignore");
  const existingIgnore = (
    await readFile(ignoreFile, "utf8").catch(() => "")
  ).replace(/\s*$/, "");
  const requiredIgnore = [
    ".conduit/runs/",
    ".conduit/cache/",
    ".conduit/worktrees/",
    ".conduit/assignments/",
  ];
  const missingIgnore = requiredIgnore.filter(
    (entry) => !existingIgnore.split(/\r?\n/).includes(entry),
  );
  if (missingIgnore.length) {
    const prefix = existingIgnore ? `${existingIgnore}\n\n` : "";
    await writeFile(
      ignoreFile,
      `${prefix}# Conduit local runtime state\n${missingIgnore.join("\n")}\n`,
    );
  }
  return { createdConfig: !configExisted, configFile };
}
