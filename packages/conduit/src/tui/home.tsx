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
import { FeatureStatusScreen } from "./screens/FeatureStatusScreen.js";
import { RoleRunSelectionScreen } from "./screens/RoleRunSelectionScreen.js";
import type { FeatureReadModel } from "@domains/features/types/feature.js";

function HomeApplication({
  commandBus,
  queryBus,
  onExit,
  projectRoot,
  updateChecksEnabled = true,
}: StartHomeParams & { onExit: () => void; projectRoot?: string }) {
  const [refiningFeatureId, setRefiningFeatureId] = useState<string | null>(
    null,
  );
  const [viewingFeatureId, setViewingFeatureId] = useState<string | null>(null);
  const [runningRunId, setRunningRunId] = useState<string | null>(null);
  const [runFeature, setRunFeature] = useState<FeatureReadModel | null>(null);
  const [statusFeature, setStatusFeature] = useState<FeatureReadModel | null>(
    null,
  );
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
  if (statusFeature)
    return (
      <FeatureStatusScreen
        queryBus={queryBus}
        projectRoot={projectRoot ?? process.cwd()}
        feature={statusFeature}
        onOpenRun={(runId) => {
          setStatusFeature(null);
          setRunningRunId(runId);
        }}
        onExit={() => setStatusFeature(null)}
      />
    );
  if (runFeature)
    return (
      <RoleRunSelectionScreen
        commandBus={commandBus}
        queryBus={queryBus}
        feature={runFeature}
        onStarted={(runId) => {
          setRunFeature(null);
          setRunningRunId(runId);
        }}
        onExit={() => setRunFeature(null)}
      />
    );
  return (
    <HomeScreen
      commandBus={commandBus}
      queryBus={queryBus}
      projectRoot={projectRoot ?? process.cwd()}
      onExit={onExit}
      onRefine={setRefiningFeatureId}
      onView={setViewingFeatureId}
      onRun={setRunFeature}
      onStatus={setStatusFeature}
      updateChecksEnabled={updateChecksEnabled}
    />
  );
}

export interface StartHomeParams {
  commandBus: CommandBus;
  queryBus: QueryBus;
  projectRoot?: string;
  updateChecksEnabled?: boolean;
}

export async function startHome(params: StartHomeParams): Promise<void> {
  const {
    commandBus,
    queryBus,
    projectRoot = process.cwd(),
    updateChecksEnabled = true,
  } = params;

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
        projectRoot={projectRoot}
        updateChecksEnabled={updateChecksEnabled}
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
