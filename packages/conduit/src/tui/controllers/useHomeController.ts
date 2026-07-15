import { useState, useEffect, useCallback, useReducer } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RolePortrait } from "@domains/roles/interfaces/role-portrait.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { conduitVersion } from "../../version.js";
import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";
import tips from "@tui/assets/tips.json" with { type: "json" };
import type {
  HomeControllerActions,
  HomeControllerState,
} from "@tui/types/home.js";
import {
  canOpenUpdateConfirmation,
  decideUpdateConfirmationKey,
  FEATURE_ACTIONS,
  homeInteractionReducer,
} from "@tui/helpers/home-interaction.js";

function randomTip(): string {
  return tips[Math.floor(Math.random() * tips.length)]!;
}

export function useHomeController(
  commandBus: CommandBus,
  queryBus: QueryBus,
  projectRoot: string,
  onExit: () => void,
  onRefine: (featureId: string) => void,
  onView: (featureId: string) => void,
  onRun: (feature: FeatureReadModel) => void,
  onStatus: (feature: FeatureReadModel) => void,
  updateChecksEnabled = true,
): [HomeControllerState, HomeControllerActions] {
  const [features, setFeatures] = useState<readonly FeatureReadModel[]>([]);
  const [portraits, setPortraits] = useState<HomeControllerState["portraits"]>(
    [],
  );
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [interaction, dispatchInteraction] = useReducer(
    homeInteractionReducer,
    {
      kind: "idle",
    },
  );
  const [tip] = useState(randomTip);
  const [updateStatus, setUpdateStatus] = useState<UpdateStatusReadModel>(
    updateChecksEnabled
      ? {
          schemaVersion: 1,
          status: UpdateStatus.Checking,
          currentVersion: conduitVersion,
        }
      : {
          schemaVersion: 1,
          status: UpdateStatus.Unavailable,
          currentVersion: conduitVersion,
          message: "Update checks are disabled for non-interactive output.",
          retryable: false,
        },
  );

  const loadData = useCallback(async () => {
    const featuresResult = await queryBus.execute({ type: "listFeatures" });
    if (featuresResult.success) {
      setFeatures(
        (featuresResult.data as { features: readonly FeatureReadModel[] })
          .features,
      );
    }

    const portraitsResult = await queryBus.execute({ type: "listPortraits" });
    if (portraitsResult.success) {
      setPortraits(
        (portraitsResult.data as { portraits: readonly RolePortrait[] })
          .portraits,
      );
    }
  }, [queryBus]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    if (!updateChecksEnabled) return;
    void queryBus.execute({ type: "checkForUpdate" }).then((result) => {
      if (result.success) {
        setUpdateStatus(result.data as UpdateStatusReadModel);
        return;
      }
      setUpdateStatus({
        schemaVersion: 1,
        status: UpdateStatus.Unavailable,
        currentVersion: conduitVersion,
        message: "The release check is unavailable.",
        retryable: false,
      });
    });
  }, [queryBus, updateChecksEnabled]);

  const searchQuery = interaction.kind === "search" ? interaction.query : "";
  const filteredFeatures = searchQuery
    ? features.filter((f) =>
        f.title.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : features;
  const setFeatureTitle = useCallback(
    (title: string) => dispatchInteraction({ type: "setTitle", value: title }),
    [],
  );
  const submitFeature = useCallback(() => {
    if (interaction.kind !== "create" || !interaction.title.trim()) return;
    void commandBus
      .dispatch({ type: "createFeature", title: interaction.title.trim() })
      .then((result) => {
        if (result.success) {
          dispatchInteraction({ type: "idle" });
          onRefine((result.data as { id: string }).id);
        }
      });
  }, [commandBus, interaction, onRefine]);

  const handleKeyDown = useCallback(
    (event: { name: string; ctrl: boolean; shift: boolean; meta: boolean }) => {
      const key = event.name ?? "";

      if (interaction.kind === "search") {
        if (key === "escape") {
          dispatchInteraction({ type: "idle" });
          return;
        }
        if (key === "return") {
          dispatchInteraction({ type: "idle" });
          return;
        }
        if (key === "backspace") {
          dispatchInteraction({ type: "backspace" });
          return;
        }
        if (key.length === 1) {
          dispatchInteraction({ type: "append", value: key });
        }
        return;
      }
      if (interaction.kind === "create") {
        if (key === "escape") {
          dispatchInteraction({ type: "idle" });
          return;
        }
        return;
      }

      if (interaction.kind === "featureActions") {
        if (key === "escape" || key === "q") {
          dispatchInteraction({ type: "idle" });
          return;
        }
        if (key === "up") {
          dispatchInteraction({ type: "previousAction" });
          return;
        }
        if (key === "down") {
          dispatchInteraction({ type: "nextAction" });
          return;
        }
        if (key === "return") {
          const action = FEATURE_ACTIONS[interaction.actionIndex];
          if (action === "View" && filteredFeatures[selectedIndex]) {
            onView(filteredFeatures[selectedIndex]!.id);
            dispatchInteraction({ type: "idle" });
            return;
          }
          if (action === "Refine" && filteredFeatures[selectedIndex]) {
            const feature = filteredFeatures[selectedIndex]!;
            void commandBus
              .dispatch({
                type: "updateFeatureMetadata",
                featureId: feature.id,
                lifecycle: "in_progress",
              })
              .finally(() => onRefine(feature.id));
          }
          if (action === "Status" && filteredFeatures[selectedIndex]) {
            onStatus(filteredFeatures[selectedIndex]!);
          }
          if (action === "Run" && filteredFeatures[selectedIndex])
            onRun(filteredFeatures[selectedIndex]!);
          dispatchInteraction({ type: "idle" });
          return;
        }
        return;
      }

      if (interaction.kind === "updateConfirmation") {
        const decision = decideUpdateConfirmationKey(interaction, key);
        if (decision?.kind === "interaction") {
          dispatchInteraction(decision.action);
          return;
        }
        if (decision?.kind === "startUpdate") {
          if (
            updateStatus.status === UpdateStatus.Available &&
            updateStatus.installation
          ) {
            void commandBus.dispatch({
              type: "startUpdate",
              release: updateStatus.release,
              installation: updateStatus.installation,
            });
          }
          dispatchInteraction({ type: "idle" });
          return;
        }
        return;
      }

      if (key === "/") {
        dispatchInteraction({ type: "search" });
        return;
      }
      if (key === "n") {
        dispatchInteraction({ type: "create" });
        return;
      }
      if (key === "u" && canOpenUpdateConfirmation(interaction, updateStatus)) {
        dispatchInteraction({ type: "openUpdateConfirmation" });
        return;
      }
      if (key === "q") {
        onExit();
        return;
      }
      if (key === "up") {
        setSelectedIndex((prev) => Math.max(0, prev - 1));
        return;
      }
      if (key === "down") {
        setSelectedIndex((prev) =>
          Math.min(filteredFeatures.length - 1, prev + 1),
        );
        return;
      }
      if (key === "return") {
        if (filteredFeatures.length > 0) {
          dispatchInteraction({ type: "actions" });
        }
        return;
      }
    },
    [
      interaction,
      selectedIndex,
      filteredFeatures,
      commandBus,
      queryBus,
      projectRoot,
      onExit,
      onRefine,
      onView,
      onRun,
      onStatus,
      updateStatus,
    ],
  );

  useKeyboard(handleKeyDown);

  return [
    {
      features,
      portraits,
      selectedIndex,
      searchQuery,
      searching: interaction.kind === "search",
      creating: interaction.kind === "create",
      featureTitle: interaction.kind === "create" ? interaction.title : "",
      actionModalOpen: interaction.kind === "featureActions",
      selectedAction:
        interaction.kind === "featureActions" ? interaction.actionIndex : 0,
      tip,
      filteredFeatures,
      updateStatus,
      updateConfirmationOpen: interaction.kind === "updateConfirmation",
      selectedUpdateAction:
        interaction.kind === "updateConfirmation" ? interaction.actionIndex : 0,
    },
    { handleKeyDown, setFeatureTitle, submitFeature },
  ];
}
