import { defaultDependencies, resolveProject } from "./shared.js";

export async function rolesCommand(options, dependencies) {
  const { output, loadConfig } = defaultDependencies(dependencies);
  const config = await loadConfig(resolveProject(options.project));
  for (const [name, role] of Object.entries(config.roles)) {
    output(
      `${name.padEnd(14)} ${role.runner.padEnd(9)} ${role.description ?? "Custom role"}\n  ${role.skill.source}`,
    );
  }
  return config.roles;
}

export async function resolveRoleCommand(name, options, dependencies) {
  const { output, progress, loadConfig, resolveSkill, builtinRoot } =
    defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const role = config.roles[name];
  if (!role) throw new Error(`Unknown role: ${name}`);
  const skill = await progress(`Resolving ${name} skill`, () =>
    resolveSkill({
      projectRoot,
      roleName: name,
      role,
      builtinRoot,
      allowNetwork: options.fetchSkills,
    }),
  );
  output(`${name}: ${skill.source} (${skill.content.length} bytes, verified)`);
  return skill;
}
