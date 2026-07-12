import { useState, useEffect, useCallback, useReducer } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RolePortrait } from "@domains/roles/interfaces/role-portrait.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import tips from "@tui/assets/tips.json" with { type: "json" };
import type {
  HomeControllerActions,
  HomeControllerState,
  HomeInteraction,
} from "@tui/types/home.js";

export const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

type HomeInteractionAction =
  | { type: "search" }
  | { type: "create" }
  | { type: "actions" }
  | { type: "idle" }
  | { type: "append"; value: string }
  | { type: "backspace" }
  | { type: "nextAction" }
  | { type: "previousAction" };

function interactionReducer(
  state: HomeInteraction,
  action: HomeInteractionAction,
): HomeInteraction {
  switch (action.type) {
    case "search":
      return { kind: "search", query: "" };
    case "create":
      return { kind: "create", title: "" };
    case "actions":
      return { kind: "featureActions", actionIndex: 0 };
    case "idle":
      return { kind: "idle" };
    case "append":
      return state.kind === "search"
        ? { ...state, query: state.query + action.value }
        : state.kind === "create"
          ? { ...state, title: state.title + action.value }
          : state;
    case "backspace":
      return state.kind === "search"
        ? { ...state, query: state.query.slice(0, -1) }
        : state.kind === "create"
          ? { ...state, title: state.title.slice(0, -1) }
          : state;
    case "nextAction":
      return state.kind === "featureActions"
        ? {
            ...state,
            actionIndex: Math.min(
              FEATURE_ACTIONS.length - 1,
              state.actionIndex + 1,
            ),
          }
        : state;
    case "previousAction":
      return state.kind === "featureActions"
        ? { ...state, actionIndex: Math.max(0, state.actionIndex - 1) }
        : state;
  }
}

function randomTip(): string {
  return tips[Math.floor(Math.random() * tips.length)]!;
}

export function useHomeController(
  commandBus: CommandBus,
  queryBus: QueryBus,
  onExit: () => void,
  onRefine: (featureId: string) => void,
  onView: (featureId: string) => void,
  onRun: (runId: string) => void,
): [HomeControllerState, HomeControllerActions] {
  const [features, setFeatures] = useState<readonly FeatureReadModel[]>([]);
  const [portraits, setPortraits] = useState<HomeControllerState["portraits"]>(
    [],
  );
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [interaction, dispatchInteraction] = useReducer(interactionReducer, {
    kind: "idle",
  });
  const [tip] = useState(randomTip);

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

  const searchQuery = interaction.kind === "search" ? interaction.query : "";
  const filteredFeatures = searchQuery
    ? features.filter((f) =>
        f.title.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : features;

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
        if (key === "backspace") {
          dispatchInteraction({ type: "backspace" });
          return;
        }
        if (key === "return" && interaction.title.trim()) {
          void commandBus
            .dispatch({ type: "createFeature", title: interaction.title })
            .then((result) => {
              if (result.success) {
                dispatchInteraction({ type: "idle" });
                onRefine((result.data as { id: string }).id);
              }
            });
          return;
        }
        if (key.length === 1)
          dispatchInteraction({ type: "append", value: key });
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
          if ((action === "Run" || action === "Status") && onRun) {
            void queryBus
              .execute({ type: "latestRuns" })
              .then((result) => {
                if (result.success) {
                  const runs = result.data as Array<{
                    id: string;
                    featureId: string;
                  }>;
                  const match = runs.find(
                    (r) => r.featureId === filteredFeatures[selectedIndex]?.id,
                  );
                  if (match) onRun(match.id);
                }
              })
              .catch(() => {});
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
      onExit,
      onRefine,
      onView,
      onRun,
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
    },
    { handleKeyDown },
  ];
}
