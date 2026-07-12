import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import type { CommandBus } from "../system/bus/command-bus.js";
import type { QueryBus } from "../system/bus/query-bus.js";
import { ThemeProvider } from "./components/ThemeProvider.js";
import { RefinementScreen } from "./screens/RefinementScreen.js";

export interface StartRefinementParams {
  commandBus: CommandBus;
  queryBus: QueryBus;
  featureId: string;
}

export async function startRefinement(
  params: StartRefinementParams,
): Promise<void> {
  const { commandBus, queryBus, featureId } = params;

  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });

  const root = createRoot(renderer);

  const handleExit = () => {
    root.unmount();
    renderer.destroy();
  };

  root.render(
    <ThemeProvider>
      <RefinementScreen
        commandBus={commandBus}
        queryBus={queryBus}
        featureId={featureId}
        onExit={handleExit}
      />
    </ThemeProvider>,
  );

  return new Promise<void>((resolve) => {
    renderer.on("exit", () => {
      resolve();
    });
  });
}
