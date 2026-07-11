import { defaultDependencies, resolveProject } from "./shared.js";
import type { RoleConfig } from "../domains/configuration/types/config.js";

export async function rolesCommand(
  options: Record<string, unknown>,
  dependencies: Record<string, unknown>,
): Promise<void> {
  const { output, loadConfig } = defaultDependencies(dependencies);
  const config = await loadConfig(
    resolveProject(options.project as string | undefined),
  );
  for (const [name, role] of Object.entries(config.roles)) {
    const r = role as RoleConfig;
    output(
      `${name.padEnd(14)} ${r.runner.padEnd(9)} ${r.description ?? "Custom role"}\n  ${r.skill.source}`,
    );
  }
}

export async function resolveRoleCommand(
  name: string,
  options: Record<string, unknown>,
  dependencies: Record<string, unknown>,
): Promise<void> {
  const { output, progress, loadConfig, resolveSkill, builtinRoot } =
    defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project as string | undefined);
  const config = await loadConfig(projectRoot);
  const role = config.roles[name];
  if (!role) throw new Error(`Unknown role: ${name}`);
  const skill = await progress(`Resolving ${name} skill`, () =>
    resolveSkill({
      projectRoot,
      roleName: name,
      role,
      builtinRoot,
      allowNetwork: options.fetchSkills as boolean | undefined,
    }),
  );
  output(`${name}: ${skill.source} (${skill.content.length} bytes, verified)`);
}
