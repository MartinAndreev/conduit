import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import { HomeFooter } from "@tui/components/HomeFooter.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { useHomeController } from "@tui/controllers/useHomeController.js";
import { FeatureActions } from "@tui/sections/FeatureActions.js";
import { Sidebar } from "@tui/sections/Sidebar.js";
import { HomeVersionStatus } from "@tui/components/HomeVersionStatus.js";
import { UpdateConfirmation } from "@tui/components/UpdateConfirmation.js";
import { UpdateStatus } from "@domains/updates/enums/update-status.js";

interface HomeScreenProps {
  commandBus: CommandBus;
  queryBus: QueryBus;
  projectRoot: string;
  onExit: () => void;
  onRefine: (featureId: string) => void;
  onView: (featureId: string) => void;
  onRun: (feature: FeatureReadModel) => void;
  onStatus: (feature: FeatureReadModel) => void;
  updateChecksEnabled: boolean;
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
  updateChecksEnabled,
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
    updateChecksEnabled,
  );

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
    >
      <HomeVersionStatus status={state.updateStatus} theme={theme} />
      <box width="100%" height="100%" flexDirection="row">
        <Sidebar
          features={state.filteredFeatures}
          selectedIndex={state.selectedIndex}
          searchQuery={state.searchQuery}
          theme={theme}
        />
        {state.updateConfirmationOpen &&
        state.updateStatus.status === UpdateStatus.Available ? (
          <UpdateConfirmation
            status={state.updateStatus}
            selectedAction={state.selectedUpdateAction}
            theme={theme}
          />
        ) : (
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
        )}
      </box>
      <HomeFooter
        searching={state.searching}
        actionModalOpen={state.actionModalOpen}
        creating={state.creating}
        updateConfirmationOpen={state.updateConfirmationOpen}
        updateAvailable={
          state.updateStatus.status === UpdateStatus.Available &&
          Boolean(state.updateStatus.installation)
        }
        theme={theme}
      />
    </box>
  );
}
