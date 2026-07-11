import type { CommandBus } from "../../system/bus/command-bus.js";
import type { QueryBus } from "../../system/bus/query-bus.js";
import { Sidebar } from "../sections/Sidebar.js";
import { FeatureActions } from "../sections/FeatureActions.js";
import { useTheme } from "../components/ThemeProvider.js";
import { useHomeController } from "../controllers/useHomeController.js";

interface HomeScreenProps {
  commandBus: CommandBus;
  queryBus: QueryBus;
  onExit: () => void;
}

export function HomeScreen({ commandBus, queryBus, onExit }: HomeScreenProps) {
  const theme = useTheme();
  const [state, actions] = useHomeController(commandBus, queryBus, onExit);

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      onKeyDown={actions.handleKeyDown}
    >
      <box
        width="100%"
        height={1}
        flexDirection="row"
        justifyContent="space-between"
      >
        <text content=" Conduit" fg={theme.text.strong} />
        <text content={`${state.tip}  `} fg={theme.text.muted} />
      </box>
      <box width="100%" height="100%" flexDirection="row">
        <Sidebar
          features={state.filteredFeatures}
          selectedIndex={state.selectedIndex}
          searchQuery={state.searchQuery}
          theme={theme}
        />
        <FeatureActions
          feature={state.filteredFeatures[state.selectedIndex]}
          portraits={state.portraits}
          theme={theme}
          actionModalOpen={state.actionModalOpen}
          selectedAction={state.selectedAction}
        />
      </box>
      <box width="100%" height={1} flexDirection="row">
        <text
          content=" [/] Search  [Enter] Actions  [q] Exit"
          fg={theme.text.muted}
        />
      </box>
    </box>
  );
}
