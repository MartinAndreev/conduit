import { defaultDependencies, resolveProject } from "./shared.js";

export async function featureCommand(
  title: string,
  options: Record<string, unknown>,
  dependencies: Record<string, unknown>,
): Promise<{ id: string; directory: string }> {
  const { output, progress, loadConfig, createFeature } =
    defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project as string | undefined);
  const config = await loadConfig(projectRoot);
  const feature = await progress("Creating feature packet", () =>
    createFeature({ projectRoot, config, title }),
  );
  output(`Created feature ${feature.id} at ${feature.directory}`);
  return feature;
}
