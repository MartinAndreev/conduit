import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import type { Root } from "@opentui/react";
import { ThemeProvider } from "./components/ThemeProvider.js";
import { theme } from "./theme.js";

interface AppProps {
  title: string;
}

function AppContent({ title }: AppProps) {
  return (
    <box
      width="100%"
      height="100%"
      backgroundColor={theme.surface.base}
      flexDirection="column"
      padding={1}
    >
      <text content={`Conduit \u00b7 ${title}`} fg={theme.text.strong} />
      <text content="" />
      <text content="Initializing..." fg={theme.text.muted} />
    </box>
  );
}

export function App({ title }: AppProps) {
  return (
    <ThemeProvider>
      <AppContent title={title} />
    </ThemeProvider>
  );
}

export interface AppHandle {
  readonly root: Root;
  update(title: string): void;
  destroy(): void;
}

export async function renderApp(title: string): Promise<AppHandle> {
  const renderer = await createCliRenderer({
    exitOnCtrlC: true,
    clearOnShutdown: true,
  });
  const root = createRoot(renderer);
  root.render(<App title={title} />);
  return {
    root,
    update(nextTitle: string) {
      root.render(<App title={nextTitle} />);
    },
    destroy() {
      root.unmount();
      renderer.destroy();
    },
  };
}

export function renderMinimalShell(title: string): string {
  return [
    `Conduit \u00b7 ${title}`,
    "",
    `  ${theme.action.primary} | ${theme.text.default}`,
    "",
  ].join("\n");
}
