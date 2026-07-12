import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import { useState } from "react";
import type { CommandBus } from "../system/bus/command-bus.js";
import type { QueryBus } from "../system/bus/query-bus.js";
import { ThemeProvider } from "./components/ThemeProvider.js";
import { HomeScreen } from "./screens/HomeScreen.js";
import { RefinementScreen } from "./screens/RefinementScreen.js";
import { FeatureDetailsScreen } from "./screens/FeatureDetailsScreen.js";
import { RunScreen } from "./screens/RunScreen.js";

function HomeApplication({
  commandBus,
  queryBus,
  onExit,
  projectRoot,
}: StartHomeParams & { onExit: () => void; projectRoot?: string }) {
  const [refiningFeatureId, setRefiningFeatureId] = useState<string | null>(
    null,
  );
  const [viewingFeatureId, setViewingFeatureId] = useState<string | null>(null);
  const [runningRunId, setRunningRunId] = useState<string | null>(null);
  if (runningRunId && projectRoot)
    return (
      <RunScreen
        commandBus={commandBus}
        queryBus={queryBus}
        projectRoot={projectRoot}
        runId={runningRunId}
        onExit={() => setRunningRunId(null)}
      />
    );
  if (refiningFeatureId)
    return (
      <RefinementScreen
        commandBus={commandBus}
        queryBus={queryBus}
        featureId={refiningFeatureId}
        onExit={() => setRefiningFeatureId(null)}
      />
    );
  if (viewingFeatureId)
    return (
      <FeatureDetailsScreen
        queryBus={queryBus}
        featureId={viewingFeatureId}
        onExit={() => setViewingFeatureId(null)}
      />
    );
  return (
    <HomeScreen
      commandBus={commandBus}
      queryBus={queryBus}
      onExit={onExit}
      onRefine={setRefiningFeatureId}
      onView={setViewingFeatureId}
      onRun={setRunningRunId}
    />
  );
}

export interface StartHomeParams {
  commandBus: CommandBus;
  queryBus: QueryBus;
  projectRoot?: string;
}

export async function startHome(params: StartHomeParams): Promise<void> {
  const { commandBus, queryBus, projectRoot } = params;

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
      <HomeApplication
        commandBus={commandBus}
        queryBus={queryBus}
        onExit={handleExit}
        projectRoot={projectRoot}
      />
    </ThemeProvider>,
  );

  return new Promise<void>((resolve) => {
    renderer.on("exit", () => {
      resolve();
    });
  });
}
