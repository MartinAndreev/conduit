import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { HomeFooter } from "@tui/components/HomeFooter.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { useHomeController } from "@tui/controllers/useHomeController.js";
import { FeatureActions } from "@tui/sections/FeatureActions.js";
import { Sidebar } from "@tui/sections/Sidebar.js";

interface HomeScreenProps {
  commandBus: CommandBus;
  queryBus: QueryBus;
  onExit: () => void;
}

export function HomeScreen({ commandBus, queryBus, onExit }: HomeScreenProps) {
  const theme = useTheme();
  const [state] = useHomeController(commandBus, queryBus, onExit);

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
    >
      <box width="100%" height="100%" flexDirection="row">
        <Sidebar
          features={state.filteredFeatures}
          selectedIndex={state.selectedIndex}
          searchQuery={state.searchQuery}
          theme={theme}
        />
        <FeatureActions
          feature={state.filteredFeatures[state.selectedIndex]}
          theme={theme}
          actionModalOpen={state.actionModalOpen}
          selectedAction={state.selectedAction}
          tip={state.tip}
        />
      </box>
      <HomeFooter
        searching={state.searching}
        actionModalOpen={state.actionModalOpen}
        theme={theme}
      />
    </box>
  );
}
