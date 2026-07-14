import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import { ThemeProvider } from "./components/ThemeProvider.js";
import { MigrationScreen } from "./screens/MigrationScreen.js";
import type { MigrationScreenState } from "./types/migration-screen.js";
import type { StartupMigrationProgress } from "@system/storage/types/startup-migration.js";

export async function runMigrationScreen<T>(
  work: (
    onProgress: (progress: StartupMigrationProgress) => void,
  ) => Promise<T>,
): Promise<T> {
  if (!process.stdin.isTTY || !process.stdout.isTTY) return work(() => {});

  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  const root = createRoot(renderer);
  let currentState: MigrationScreenState = {
    stage: "global-schema",
    message: "Preparing storage",
    completed: 0,
    total: 3,
  };
  const render = (state: MigrationScreenState) => {
    currentState = state;
    root.render(
      <ThemeProvider>
        <MigrationScreen state={state} />
      </ThemeProvider>,
    );
  };
  render(currentState);
  try {
    return await work(render);
  } catch (error) {
    render({
      stage: currentState.stage,
      message: "Migration failed",
      completed: currentState.completed,
      total: currentState.total,
      error: error instanceof Error ? error.message : String(error),
    });
    await new Promise<void>((resolve) => {
      const onKeypress = (key: { name?: string; ctrl?: boolean }) => {
        if (
          key.name === "return" ||
          key.name === "escape" ||
          key.name === "q" ||
          (key.ctrl && key.name === "c")
        ) {
          renderer.keyInput.off("keypress", onKeypress);
          resolve();
        }
      };
      renderer.keyInput.on("keypress", onKeypress);
    });
    throw error;
  } finally {
    root.unmount();
    renderer.destroy();
  }
}
