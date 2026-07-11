import type { CommandBus } from "../../../system/bus/command-bus.js";
import type { QueryBus } from "../../../system/bus/query-bus.js";

export interface RefineCommandReactOptions {
  featureId: string;
  story?: string;
  testCases?: string;
  architect?: boolean;
  compact?: boolean;
  interactive?: boolean;
}

export async function refineCommandReact(
  options: RefineCommandReactOptions,
  deps: {
    commandBus: CommandBus;
    queryBus: QueryBus;
    startRefinementScreen: (params: {
      commandBus: CommandBus;
      queryBus: QueryBus;
      featureId: string;
    }) => Promise<void>;
    output: (message: string) => void;
  },
): Promise<void> {
  const { commandBus, queryBus, startRefinementScreen, output } = deps;

  // If not interactive or compact, use the legacy command
  if (options.interactive === false || options.compact) {
    output(
      "Compact mode: use the legacy refine command for non-interactive refinement.",
    );
    return;
  }

  // Check if there's an existing draft
  const draftResult = await queryBus.execute({
    type: "getDraft",
    featureId: options.featureId,
  });

  if (draftResult.success) {
    const data = draftResult.data as { draft: { featureId: string } | null };
    if (data.draft) {
      output(
        `Found existing draft for feature ${options.featureId}. Resuming...`,
      );
    }
  }

  // Start the React refinement screen
  await startRefinementScreen({
    commandBus,
    queryBus,
    featureId: options.featureId,
  });
}
