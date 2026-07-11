import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import type { CommandBus } from "../system/bus/command-bus.js";
import type { QueryBus } from "../system/bus/query-bus.js";
import { ThemeProvider } from "./components/ThemeProvider.js";
import { HomeScreen } from "./screens/HomeScreen.js";

export interface StartHomeParams {
  commandBus: CommandBus;
  queryBus: QueryBus;
}

export async function startHome(params: StartHomeParams): Promise<void> {
  const { commandBus, queryBus } = params;

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
      <HomeScreen
        commandBus={commandBus}
        queryBus={queryBus}
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
