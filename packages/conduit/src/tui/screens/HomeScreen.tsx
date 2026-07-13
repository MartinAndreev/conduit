import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import { HomeFooter } from "@tui/components/HomeFooter.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { useHomeController } from "@tui/controllers/useHomeController.js";
import { FeatureActions } from "@tui/sections/FeatureActions.js";
import { Sidebar } from "@tui/sections/Sidebar.js";

interface HomeScreenProps {
  commandBus: CommandBus;
  queryBus: QueryBus;
  projectRoot: string;
  onExit: () => void;
  onRefine: (featureId: string) => void;
  onView: (featureId: string) => void;
  onRun: (feature: FeatureReadModel) => void;
  onStatus: (feature: FeatureReadModel) => void;
}

export function HomeScreen({
  commandBus,
  queryBus,
  projectRoot,
  onExit,
  onRefine,
  onView,
  onRun,
  onStatus,
}: HomeScreenProps) {
  const theme = useTheme();
  const [state, actions] = useHomeController(
    commandBus,
    queryBus,
    projectRoot,
    onExit,
    onRefine,
    onView,
    onRun,
    onStatus,
  );

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
          creating={state.creating}
          featureTitle={state.featureTitle}
          setFeatureTitle={actions.setFeatureTitle}
          submitFeature={actions.submitFeature}
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
