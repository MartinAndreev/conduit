import { defaultDependencies, resolveProject } from "./shared.js";

export async function featureCommand(title, options, dependencies) {
  const { output, progress, loadConfig, createFeature } =
    defaultDependencies(dependencies);
  const projectRoot = resolveProject(options.project);
  const config = await loadConfig(projectRoot);
  const feature = await progress("Creating feature packet", () =>
    createFeature({ projectRoot, config, title }),
  );
  output(`Created feature ${feature.id} at ${feature.directory}`);
  return feature;
}
