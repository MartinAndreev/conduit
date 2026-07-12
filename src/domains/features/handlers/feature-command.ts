import {
  defaultDependencies,
  resolveProject,
} from "../../../system/cli/command-support.js";
import type { CommandRuntimeDependencies } from "../../../system/cli/command-support.js";
import type { ApplicationDependencies } from "../../../system/bootstrap/types.js";

type FeatureCommandDependencies = Pick<
  ApplicationDependencies,
  "loadConfig" | "createFeature"
> &
  Partial<CommandRuntimeDependencies>;

export async function featureCommand(
  title: string,
  options: Record<string, unknown>,
  dependencies: Partial<FeatureCommandDependencies>,
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
